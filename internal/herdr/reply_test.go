package herdr

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// oversizedReply is one JSON reply just past MaxReplyBytes.
func oversizedReply() string {
	return `{"id":"x","result":{"name":"` + strings.Repeat("x", MaxReplyBytes) + `"}}` + "\n"
}

func TestReplyDecoderBoundsEachReplyNotTheConnection(t *testing.T) {
	half := `{"name":"` + strings.Repeat("x", MaxReplyBytes/2) + `"}` + "\n"
	decoder := newReplyDecoder(strings.NewReader(half + half + half))
	for index := range 3 {
		var value struct{ Name string }
		if err := decoder.Decode(&value); err != nil || len(value.Name) != MaxReplyBytes/2 {
			t.Fatalf("reply %d = %d bytes, %v", index, len(value.Name), err)
		}
	}
	var value any
	if err := newReplyDecoder(strings.NewReader(oversizedReply())).Decode(&value); !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("oversized reply error = %v", err)
	}
}

// serveOneReply answers the first request on a fresh socket with reply.
func serveOneReply(t *testing.T, reply string) string {
	t.Helper()
	path := shortSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_, _ = io.WriteString(conn, reply)
	}()
	return path
}

func TestOversizedHerdrRepliesFailClearly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := NewClient(serveOneReply(t, oversizedReply()), logger).Snapshot(context.Background())
	if !errors.Is(err, ErrReplyTooLarge) || !strings.Contains(err.Error(), "larger than 16 MiB") {
		t.Fatalf("Herdr TUI snapshot error = %v", err)
	}
	_, err = NewCoreClient(serveOneReply(t, oversizedReply()), logger).Read(context.Background())
	if !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("Herdr Core read error = %v", err)
	}
}

func TestOversizedHerdrAppReplyFailsClearly(t *testing.T) {
	app, client := newFakeApp(t)
	app.answer(t, 0, `{"name":"`+strings.Repeat("x", MaxReplyBytes)+`"}`, "")
	var result map[string]any
	err := client.Do(context.Background(), "tab.rename", map[string]any{}, &result)
	var mutation *MutationError
	if !errors.As(err, &mutation) || !mutation.Submitted || !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("oversized app reply error = %#v", err)
	}
}
