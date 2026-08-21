package notifications

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

type blockingLogWriter struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func newBlockingLogWriter() *blockingLogWriter {
	return &blockingLogWriter{entered: make(chan struct{}), release: make(chan struct{})}
}

func (w *blockingLogWriter) Write(data []byte) (int, error) {
	w.enteredOnce.Do(func() { close(w.entered) })
	<-w.release
	return len(data), nil
}

func (w *blockingLogWriter) unblock() {
	w.releaseOnce.Do(func() { close(w.release) })
}

func TestObserveSnapshotDropsSaturatedWorkWithoutBlockingOnLogging(t *testing.T) {
	writer := newBlockingLogWriter()
	defer writer.unblock()
	manager := NewManager(&Store{}, slog.New(slog.NewTextHandler(writer, nil)))
	for len(manager.events) < cap(manager.events) {
		manager.events <- Event{Kind: EventWorkspaceOpened}
	}
	manager.ObserveSnapshot(notificationSnapshot("working", "w1"), true)

	done := make(chan struct{})
	go func() {
		manager.ObserveSnapshot(notificationSnapshot("blocked", "w1"), false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("saturated notification enqueue blocked Home publication")
	}
	select {
	case <-writer.entered:
		t.Fatal("saturated notification enqueue wrote a synchronous log")
	default:
	}
	if got := len(manager.events); got != cap(manager.events) {
		t.Fatalf("saturated queue length = %d, want dropped work and length %d", got, cap(manager.events))
	}
}
