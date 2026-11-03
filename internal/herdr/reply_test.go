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

// withReplyLimit sets a small reply limit for one test.
func withReplyLimit(t *testing.T, limit int64) {
	previous := replyLimit
	SetReplyLimit(limit)
	t.Cleanup(func() { SetReplyLimit(previous) })
}

// oversizedReply is one JSON reply just past the reply limit.
func oversizedReply() string {
	return `{"id":"x","result":{"name":"` + strings.Repeat("x", int(replyLimit)) + `"}}` + "\n"
}

func TestReplyDecoderBoundsEachReplyNotTheConnection(t *testing.T) {
	withReplyLimit(t, 1<<20)
	half := `{"name":"` + strings.Repeat("x", 1<<19) + `"}` + "\n"
	decoder := newReplyDecoder(strings.NewReader(half + half + half))
	for index := range 3 {
		var value struct{ Name string }
		if err := decoder.Decode(&value); err != nil || len(value.Name) != 1<<19 {
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
	withReplyLimit(t, 3<<20)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := NewClient(serveOneReply(t, oversizedReply()), logger).Snapshot(context.Background())
	if !errors.Is(err, ErrReplyTooLarge) || !strings.Contains(err.Error(), "larger than 3 MiB") {
		t.Fatalf("Herdr TUI snapshot error = %v", err)
	}
	_, err = NewCoreClient(serveOneReply(t, oversizedReply()), logger).Read(context.Background())
	if !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("Herdr Core read error = %v", err)
	}
}

func TestOversizedHerdrAppReplyFailsClearly(t *testing.T) {
	withReplyLimit(t, 1<<20)
	app, client := newFakeApp(t)
	app.answer(t, 0, `{"name":"`+strings.Repeat("x", 1<<20)+`"}`, "")
	var result map[string]any
	err := client.Do(context.Background(), "tab.rename", map[string]any{}, &result)
	var mutation *MutationError
	if !errors.As(err, &mutation) || !mutation.Submitted || !errors.Is(err, ErrReplyTooLarge) {
		t.Fatalf("oversized app reply error = %#v", err)
	}
}

func TestReplyLimitDefaultAndMessage(t *testing.T) {
	if DefaultReplyLimit != 64<<20 || replyLimit != DefaultReplyLimit {
		t.Fatalf("default reply limit = %d, current %d", DefaultReplyLimit, replyLimit)
	}
	for size, want := range map[int64]string{64 << 20: "64 MiB", 2 << 30: "2 GiB", 1536 << 10: "1536 KiB", 1<<20 + 1: "1048577 bytes"} {
		if got := (replyTooLarge{size}).Error(); got != "Herdr sent a reply larger than "+want {
			t.Fatalf("message for %d = %q", size, got)
		}
	}
}
