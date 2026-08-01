package androidagent

import (
	"context"
	"errors"
	"testing"
)

func TestRemoteStreamWaitOpenedReturnsDialError(t *testing.T) {
	stream := newRemoteStream(nil, "stream-1", "tcp", "example.test:80")
	want := errors.New("cellular dial failed")
	stream.openResult(want)
	stream.closeWithError(want)

	if err := stream.waitOpened(context.Background()); err == nil || err.Error() != want.Error() {
		t.Fatalf("waitOpened error = %v, want %v", err, want)
	}
}
