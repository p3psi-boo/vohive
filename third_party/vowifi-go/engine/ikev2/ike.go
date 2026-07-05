package ikev2

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/bufferpool"
	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/logger"
)

const (
	ikeHeaderLen        = 28
	maxRetransmit       = 5
	retransmitBaseDelay = 500 * time.Millisecond
	DefaultPort         = 500
	NatTPort            = 4500
	ikeVersion          = 0x20
)

type Session struct {
	mu        sync.Mutex
	cfg       *Config
	ikesa     *IKESA
	conn      net.Conn
	localAddr net.Addr
	remoteAddr net.Addr
	eapIdentity *eap.ChallengeRequest
	akaprovider eap.AKAProvider
	natDetected bool
	msgID      uint32
	saInitReq  []byte
	saInitResp []byte
}

func NewSession(cfg *Config, conn net.Conn, aka eap.AKAProvider) *Session {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Session{
		cfg:         cfg,
		conn:        conn,
		localAddr:   conn.LocalAddr(),
		remoteAddr:  conn.RemoteAddr(),
		akaprovider: aka,
	}
}

func (s *Session) Negotiate(ctx context.Context) (*IKESA, error) {
	if err := s.ikeSAInit(ctx); err != nil {
		return nil, fmt.Errorf("ike_sa_init failed: %w", err)
	}
	if err := s.ikeAuth(ctx); err != nil {
		return nil, fmt.Errorf("ike_auth failed: %w", err)
	}
	return s.ikesa, nil
}

func (s *Session) ikeSAInit(ctx context.Context) error {
	ikesa := &IKESA{
		State: StateInit,
	}
	s.ikesa = ikesa
	s.cfg.NonceSize = 32

	dhGroup := s.selectDHGroup()
	dhKey, err := crypto.DHGenerate(int(dhGroup))
	if err != nil {
		return fmt.Errorf("dh generate: %w", err)
	}
	ikesa.DHGroup = TransformID(dhKey.Group)
	ikesa.DHPrivate = dhKey.PrivateKey
	ikesa.DHPublic = dhKey.PublicKey
	ikesa.LocalNonce = mustGenerateRandom(s.cfg.NonceSize)
	ikesa.LocalSPI = mustGenerateSPI()
	ikesa.InitiatorSPI = ikesa.LocalSPI
	ikesa.CreatedAt = time.Now()

	msg1 := &IKEMessage{
		InitiatorSPI: ikesa.LocalSPI,
		ResponderSPI: 0,
		NextPayload:  PayloadSA,
		Version:      ikeVersion,
		ExchangeType: ExchangeIKE_SA_INIT,
		Flags:        0x08,
		MessageID:    0,
		Payloads: []Payload{
			{Type: PayloadSA, Data: buildSAProposals(s.cfg.Proposals)},
			{Type: PayloadKE, Data: buildKEPayload(dhKey.Group, dhKey.PublicKey)},
			{Type: PayloadNonce, Data: ikesa.LocalNonce},
		},
	}
	if s.cfg.UseMOBIKE {
		msg1.Payloads = append(msg1.Payloads, Payload{Type: PayloadNotify, Data: buildNotify(NotifyMOBIKE_SUPPORTED)})
	}
	// 不在 IKE_SA_INIT 中添加 NAT detection (对照 SimAdmin live.rs)

	raw := serializeIKE(msg1)
	s.saInitReq = raw
	logger.Info("IKE_SA_INIT request", "len", len(raw), "spi_i", fmt.Sprintf("%016x", ikesa.LocalSPI))
	logger.Info("IKE_SA_INIT hex", "payload", fmt.Sprintf("%x", raw))
	if err := s.sendWithRetry(ctx, raw); err != nil {
		return fmt.Errorf("send IKE_SA_INIT: %w", err)
	}
	ikesa.State = StateSAInitSent

	rawResp, err := s.recvWithRetry(ctx)
	if err != nil {
		return fmt.Errorf("recv IKE_SA_INIT resp: %w", err)
	}
	resp, err := parseIKE(rawResp)
	if err != nil {
		return fmt.Errorf("parse IKE_SA_INIT resp: %w", err)
	}
	if resp.ExchangeType != ExchangeIKE_SA_INIT {
		return fmt.Errorf("unexpected exchange type: %d", resp.ExchangeType)
	}
	ikesa.ResponderSPI = resp.InitiatorSPI

	for _, p := range resp.Payloads {
		switch p.Type {
		case PayloadSA:
			ikesa.Proposals = parseProposals(p.Data)
		case PayloadKE:
			group, pub := parseKE(p.Data)
			ikesa.RemoteDHPublic = pub
			ikesa.DHGroup = TransformID(group)
		case PayloadNonce:
			ikesa.RemoteNonce = p.Data
		}
	}
	if ikesa.RemoteDHPublic == nil || ikesa.RemoteNonce == nil {
		return fmt.Errorf("missing KE or Nonce in response")
	}

	shared, err := crypto.DHCompute(int(ikesa.DHGroup), ikesa.DHPrivate, ikesa.RemoteDHPublic)
	if err != nil {
		return fmt.Errorf("dh compute: %w", err)
	}
	ikesa.SharedSecret = shared

	ikesa.Keys = deriveIKEKeyMaterial(ikesa.LocalNonce, ikesa.RemoteNonce, shared, ikesa.LocalSPI, ikesa.ResponderSPI)
	ikesa.State = StateSAInitRcvd
	s.saInitResp = rawResp
	return nil
}

func (s *Session) ikeAuth(ctx context.Context) error {
	ikesa := s.ikesa
	if ikesa == nil {
		return fmt.Errorf("no IKE SA")
	}

	authPayload := buildEAPIdentityPayload()
	saFirst := s.composeAuthRequest(authPayload)
	raw := serializeEncryptedIKE(saFirst, ikesa.Keys.SK_ei, ikesa.Keys.SK_ai, ikesa.InitiatorSPI, ikesa.ResponderSPI, ikesa.MessageID+1)
	if err := s.sendWithRetry(ctx, raw); err != nil {
		return fmt.Errorf("send IKE_AUTH request: %w", err)
	}
	ikesa.MessageID++
	ikesa.State = StateAuthSent

	rawResp, err := s.recvWithRetry(ctx)
	if err != nil {
		return fmt.Errorf("recv IKE_AUTH resp: %w", err)
	}
	resp, err := parseEncryptedIKE(rawResp, ikesa.Keys.SK_er, ikesa.Keys.SK_ar, ikesa.ResponderSPI, ikesa.InitiatorSPI)
	if err != nil {
		return fmt.Errorf("parse IKE_AUTH resp: %w", err)
	}

	challenge, err := extractEAPChallenge(resp.Payloads)
	if err != nil {
		return fmt.Errorf("extract EAP challenge: %w", err)
	}

	if s.akaprovider == nil {
		return fmt.Errorf("no AKA provider for EAP-AKA")
	}
	eapResult, err := eap.RunEAPAKA(s.akaprovider, challenge)
	if err != nil {
		return fmt.Errorf("eap-aka: %w", err)
	}
	ikesa.MSK = eapResult.MSK

	ikeAuth := &IKEMessage{
		InitiatorSPI: ikesa.InitiatorSPI,
		ResponderSPI: ikesa.ResponderSPI,
		NextPayload:  PayloadEAP,
		Version:      ikeVersion,
		ExchangeType: ExchangeIKE_AUTH,
		MessageID:    ikesa.MessageID + 1,
	}
	ikeAuth.Payloads = append(ikeAuth.Payloads, Payload{Type: PayloadEAP, Data: buildEAPResponse(eapResult, challenge)})
	idiPayload := buildIdentity(s.cfg.LocalIdentity)
	ikeAuth.Payloads = append(ikeAuth.Payloads, Payload{Type: PayloadAUTH, Data: buildAUTHPayload(eapResult.MSK, ikesa, s.saInitReq, idiPayload, ikesa.RemoteNonce)})

	raw3 := serializeEncryptedIKE(ikeAuth, ikesa.Keys.SK_ei, ikesa.Keys.SK_ai, ikesa.InitiatorSPI, ikesa.ResponderSPI, ikeAuth.MessageID)
	if err := s.sendWithRetry(ctx, raw3); err != nil {
		return fmt.Errorf("send IKE_AUTH EAP response: %w", err)
	}
	ikesa.MessageID++

	raw4, err := s.recvWithRetry(ctx)
	if err != nil {
		return fmt.Errorf("recv IKE_AUTH final: %w", err)
	}
	authResp, err := parseEncryptedIKE(raw4, ikesa.Keys.SK_er, ikesa.Keys.SK_ar, ikesa.ResponderSPI, ikesa.InitiatorSPI)
	if err != nil {
		return fmt.Errorf("IKE_AUTH final response parse: %w", err)
	}
	if authResp != nil {
		for _, p := range authResp.Payloads {
			if p.Type == PayloadEAP && isEAPSuccess(p.Data) {
				ikesa.State = StateEstablished
				ikesa.LastActivity = time.Now()
				logger.Info("IKE_AUTH established", "local_spi", ikesa.LocalSPI, "remote_spi", ikesa.ResponderSPI)
				return nil
			}
		}
	}
	return fmt.Errorf("IKE_AUTH failed: no EAP-Success received")
}

func (s *Session) CreateChildSA(ctx context.Context, trafficSelectors []TrafficSelector) (*ChildSA, error) {
	ikesa := s.ikesa
	if ikesa == nil || ikesa.State != StateEstablished {
		return nil, fmt.Errorf("IKE SA not established")
	}
	ikesa.mu.Lock()
	ikesa.MessageID++
	msgID := ikesa.MessageID
	ikesa.mu.Unlock()

	dhKey, err := crypto.DHGenerate(int(ikesa.DHGroup))
	if err != nil {
		return nil, fmt.Errorf("child DH: %w", err)
	}

	msg := &IKEMessage{
		InitiatorSPI: ikesa.InitiatorSPI,
		ResponderSPI: ikesa.ResponderSPI,
		NextPayload:  PayloadSA,
		Version:      ikeVersion,
		ExchangeType: ExchangeCREATE_CHILD_SA,
		MessageID:    msgID,
	}
	msg.Payloads = append(msg.Payloads, Payload{Type: PayloadSA, Data: buildChildSAProposal()})
	msg.Payloads = append(msg.Payloads, Payload{Type: PayloadNonce, Data: mustGenerateRandom(s.cfg.NonceSize)})
	if dhKey != nil {
		msg.Payloads = append(msg.Payloads, Payload{Type: PayloadKE, Data: buildKEPayload(dhKey.Group, dhKey.PublicKey)})
	}
	msg.Payloads = append(msg.Payloads, Payload{Type: PayloadTSi, Data: buildTS(trafficSelectors)})
	msg.Payloads = append(msg.Payloads, Payload{Type: PayloadTSr, Data: buildTS(trafficSelectors)})

	raw := serializeEncryptedIKE(msg, ikesa.Keys.SK_ei, ikesa.Keys.SK_ai, ikesa.InitiatorSPI, ikesa.ResponderSPI, msgID)
	if err := s.sendWithRetry(ctx, raw); err != nil {
		return nil, fmt.Errorf("send CREATE_CHILD_SA: %w", err)
	}

	respRaw, err := s.recvWithRetry(ctx)
	if err != nil {
		return nil, fmt.Errorf("recv CREATE_CHILD_SA resp: %w", err)
	}
	resp, _ := parseEncryptedIKE(respRaw, ikesa.Keys.SK_er, ikesa.Keys.SK_ar, ikesa.ResponderSPI, ikesa.InitiatorSPI)
	if resp == nil {
		return nil, fmt.Errorf("invalid CREATE_CHILD_SA response")
	}

	var spi uint32
	for _, p := range resp.Payloads {
		if p.Type == PayloadSA {
			for _, prop := range parseProposals(p.Data) {
				if prop.ProtocolID == ProtocolESP && len(prop.SPI) >= 4 {
					spi = binary.BigEndian.Uint32(prop.SPI[:4])
				}
			}
		}
	}
	if spi == 0 {
		return nil, fmt.Errorf("no ESP SPI in response")
	}

	child := &ChildSA{
		SPI:                spi,
		Protocol:           ProtocolESP,
		TrafficSelectorsIn: trafficSelectors,
	}
	if ikesa.ChildSAs == nil {
		ikesa.ChildSAs = make(map[uint32]*ChildSA)
	}
	ikesa.ChildSAs[spi] = child
	logger.Info("Child SA created", "spi", spi)
	return child, nil
}

func (s *Session) Close() {
	if s.ikesa != nil {
		s.ikesa.State = StateClosed
	}
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *Session) sendWithRetry(ctx context.Context, data []byte) error {
	for i := 0; i < maxRetransmit; i++ {
		if _, err := s.conn.Write(data); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retransmitBaseDelay * time.Duration(1<<i)):
		}
	}
	return fmt.Errorf("max retransmit exceeded")
}

func (s *Session) recvWithRetry(ctx context.Context) ([]byte, error) {
	buf := bufferpool.Get(65535)
	defer bufferpool.Put(buf)
	attempt := 0
	for i := 0; i < maxRetransmit; i++ {
		s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		attempt++
		n, err := s.conn.Read(buf)
		if err == nil {
			data := make([]byte, n)
			copy(data, buf[:n])
			logger.Info("IKE recv success", "attempt", attempt, "bytes", n)
			return data, nil
		}
		logger.Info("IKE recv retry", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return nil, fmt.Errorf("max recv retry exceeded")
}

func (s *Session) selectDHGroup() TransformID {
	if len(s.cfg.DHGroups) > 0 {
		return s.cfg.DHGroups[0]
	}
	return DHGroup14_2048
}

func (s *Session) composeAuthRequest(eapPayload []byte) *IKEMessage {
	ikesa := s.ikesa
	msg := &IKEMessage{
		InitiatorSPI: ikesa.InitiatorSPI,
		ResponderSPI: ikesa.ResponderSPI,
		NextPayload:  PayloadIDi,
		Version:      ikeVersion,
		ExchangeType: ExchangeIKE_AUTH,
		Flags:        0x08,
		MessageID:    ikesa.MessageID + 1,
	}
	msg.Payloads = append(msg.Payloads, Payload{Type: PayloadIDi, Data: buildIdentity(s.cfg.LocalIdentity)})
	if eapPayload != nil {
		msg.Payloads = append(msg.Payloads, Payload{Type: PayloadEAP, Data: eapPayload})
	}
	msg.Payloads = append(msg.Payloads, s.buildTrafficSelectors()...)
	return msg
}

func (s *Session) appendNATDetection(payloads []Payload) []Payload {
	p := make([]Payload, len(payloads))
	copy(p, payloads)
	srcHash := buildNATHash(s.ikesa.LocalSPI, s.ikesa.ResponderSPI, s.localAddr.String(), DefaultPort)
	dstHash := buildNATHash(s.ikesa.LocalSPI, s.ikesa.ResponderSPI, s.remoteAddr.String(), DefaultPort)
	p = append(p, Payload{Type: PayloadNotify, Data: buildNotifyData(NotifyNAT_DETECTION_SOURCE_IP, srcHash)})
	p = append(p, Payload{Type: PayloadNotify, Data: buildNotifyData(NotifyNAT_DETECTION_DESTINATION_IP, dstHash)})
	return p
}

func (s *Session) buildTrafficSelectors() []Payload {
	return []Payload{
		{Type: PayloadTSi, Data: buildTS(nil)},
		{Type: PayloadTSr, Data: buildTS(nil)},
	}
}

func hashAddr(addr net.Addr) []byte {
	if addr == nil {
		return []byte{}
	}
	return []byte(addr.String())
}

func mustGenerateRandom(n int) []byte {
	b, _ := crypto.GenerateRandom(n)
	return b
}

func mustGenerateSPI() uint64 {
	b, _ := crypto.GenerateRandom(8)
	return binary.BigEndian.Uint64(b)
}
