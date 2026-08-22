package notifications

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/luisc/shepherdr/internal/herdr"
)

func TestDeliveryDiagnosticsReportQueueAndPushOutcomesWithoutPrivateData(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/private-endpoint", EventSettings{Blocked: true})
	if err := store.Upsert(subscription); err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	manager := NewManager(store, slog.New(slog.NewTextHandler(&logs, nil)))

	manager.ObserveSnapshot(notificationSnapshot("working", "w1"), true)
	manager.ObserveSnapshot(notificationSnapshot("blocked", "w1"), false)
	for len(manager.events) < cap(manager.events) {
		manager.events <- Event{Kind: EventWorkspaceOpened}
	}
	manager.ObserveSnapshot(notificationSnapshot("done", "w1"), false)
	if got := len(manager.events); got != cap(manager.events) {
		t.Fatalf("saturated queue length = %d, want dropped work and length %d", got, cap(manager.events))
	}

	event := Event{
		Kind: EventStatus, Status: herdr.StatusBlocked, WorkspaceName: "Private workspace",
		PaneID: "private-pane", TerminalID: "private-terminal", Destination: "/#private-destination",
	}
	for _, outcome := range []sendOutcome{sendAccepted, sendFailed, sendGone} {
		manager.sender = fixedSender{outcome: outcome}
		manager.deliverEvent(context.Background(), event)
	}

	got := logs.String()
	for _, message := range []string{
		"Notification candidate observed",
		"Notification candidate enqueued",
		"Notification candidate dropped",
		"Push service accepted notification",
		"Notification push failed",
		"Push service reports subscription gone",
	} {
		if !strings.Contains(got, message) {
			t.Errorf("diagnostics omitted %q: %s", message, got)
		}
	}
	for _, private := range []string{
		"Workspace w1", "Private workspace", "w1:p1", "term-1", "private-pane", "private-terminal",
		"private-destination", subscription.Endpoint, subscription.Keys.Auth, subscription.Keys.P256dh,
	} {
		if strings.Contains(got, private) {
			t.Errorf("diagnostics exposed private value %q: %s", private, got)
		}
	}
}
