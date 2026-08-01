package androidagent

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

type remoteStream struct {
	session *Session
	id      string
	network string
	address string

	opened chan error
	readCh chan []byte
	done   chan struct{}
	once   sync.Once

	mu                  sync.Mutex
	readBuf             []byte
	readErr             error
	readDeadline        time.Time
	writeDeadline       time.Time
	readDeadlineChanged chan struct{}
}

func newRemoteStream(session *Session, id, network, address string) *remoteStream {
	return &remoteStream{
		session:             session,
		id:                  id,
		network:             network,
		address:             address,
		opened:              make(chan error, 1),
		readCh:              make(chan []byte, 32),
		done:                make(chan struct{}),
		readDeadlineChanged: make(chan struct{}),
	}
}

func (s *remoteStream) waitOpened(ctx context.Context) error {
	select {
	case err := <-s.opened:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		s.mu.Lock()
		err := s.readErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}
}

func (s *remoteStream) openResult(err error) {
	select {
	case s.opened <- err:
	default:
	}
}

func (s *remoteStream) enqueue(payload []byte) {
	if len(payload) == 0 {
		return
	}
	copyPayload := append([]byte(nil), payload...)
	select {
	case s.readCh <- copyPayload:
	case <-s.done:
	}
}

func (s *remoteStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	if len(s.readBuf) > 0 {
		n := copy(p, s.readBuf)
		s.readBuf = s.readBuf[n:]
		s.mu.Unlock()
		return n, nil
	}
	err := s.readErr
	s.mu.Unlock()
	if err != nil {
		select {
		case data := <-s.readCh:
			return s.copyReadData(p, data), nil
		default:
			return 0, err
		}
	}

	for {
		s.mu.Lock()
		deadline := s.readDeadline
		deadlineChanged := s.readDeadlineChanged
		s.mu.Unlock()
		deadlineC, stopTimer := streamDeadline(deadline)
		select {
		case data := <-s.readCh:
			stopTimer()
			return s.copyReadData(p, data), nil
		case <-s.done:
			stopTimer()
			// WebSocket frames are handled in order. Drain any data frame that
			// arrived before stream_close before exposing EOF to net/http.
			select {
			case data := <-s.readCh:
				return s.copyReadData(p, data), nil
			default:
			}
			s.mu.Lock()
			err = s.readErr
			s.mu.Unlock()
			if err == nil {
				err = io.EOF
			}
			return 0, err
		case <-deadlineC:
			stopTimer()
			return 0, os.ErrDeadlineExceeded
		case <-deadlineChanged:
			stopTimer()
			// SetReadDeadline applies to an already-blocked Read, so rebuild
			// the timer using the new value.
		}
	}
}

func (s *remoteStream) copyReadData(p, data []byte) int {
	n := copy(p, data)
	if n < len(data) {
		s.mu.Lock()
		s.readBuf = append(s.readBuf, data[n:]...)
		s.mu.Unlock()
	}
	return n
}

func (s *remoteStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	select {
	case <-s.done:
		return 0, io.ErrClosedPipe
	default:
	}
	s.mu.Lock()
	deadline := s.writeDeadline
	s.mu.Unlock()
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	if err := s.session.send(Message{Type: MessageStreamData, StreamID: s.id, Data: base64.StdEncoding.EncodeToString(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *remoteStream) Close() error {
	if s == nil {
		return nil
	}
	_ = s.session.send(Message{Type: MessageStreamClose, StreamID: s.id})
	s.session.removeStream(s.id)
	s.closeWithError(io.EOF)
	return nil
}

func (s *remoteStream) closeWithError(err error) {
	if err == nil {
		err = io.EOF
	}
	s.once.Do(func() {
		s.mu.Lock()
		s.readErr = err
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *remoteStream) LocalAddr() net.Addr  { return streamAddr("vohive") }
func (s *remoteStream) RemoteAddr() net.Addr { return streamAddr(s.address) }

func (s *remoteStream) SetDeadline(t time.Time) error {
	s.mu.Lock()
	s.readDeadline = t
	s.writeDeadline = t
	s.signalReadDeadlineChangedLocked()
	s.mu.Unlock()
	return nil
}
func (s *remoteStream) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.readDeadline = t
	s.signalReadDeadlineChangedLocked()
	s.mu.Unlock()
	return nil
}

func (s *remoteStream) signalReadDeadlineChangedLocked() {
	close(s.readDeadlineChanged)
	s.readDeadlineChanged = make(chan struct{})
}
func (s *remoteStream) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	s.writeDeadline = t
	s.mu.Unlock()
	return nil
}

func streamDeadline(deadline time.Time) (<-chan time.Time, func()) {
	if deadline.IsZero() {
		return nil, func() {}
	}
	timer := time.NewTimer(time.Until(deadline))
	return timer.C, func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

type streamAddr string

func (a streamAddr) Network() string { return "tcp" }
func (a streamAddr) String() string  { return string(a) }

var _ net.Conn = (*remoteStream)(nil)
