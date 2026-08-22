package notifications

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luisc/shepherdr/internal/herdr"
)

type testTrustAuthority struct {
	active bool
	calls  int
}

func (a *testTrustAuthority) WithActiveTrustRead(_ string, operation func() error) error {
	a.calls++
	if !a.active {
		return errors.New("inactive")
	}
	return operation()
}

type countingSender struct{ calls int }

func (s *countingSender) Send(context.Context, Event, Subscription, string, string, string) sendOutcome {
	s.calls++
	return sendAccepted
}

type selectiveTrustAuthority struct {
	mu     sync.RWMutex
	active map[string]bool
}

func (a *selectiveTrustAuthority) WithActiveTrustRead(trustID string, operation func() error) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.active[trustID] {
		return errors.New("inactive")
	}
	return operation()
}

func (a *selectiveTrustAuthority) setActive(trustID string, active bool) {
	a.mu.Lock()
	a.active[trustID] = active
	a.mu.Unlock()
}

type capturedSend struct {
	event   Event
	trustID string
}

type captureSender struct {
	mu    sync.Mutex
	sends []capturedSend
}

func (s *captureSender) Send(_ context.Context, event Event, subscription Subscription, _, _, _ string) sendOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends = append(s.sends, capturedSend{event: event, trustID: subscription.TrustID})
	return sendAccepted
}

func (s *captureSender) take() []capturedSend {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]capturedSend(nil), s.sends...)
	s.sends = nil
	return result
}

func TestAccessChangeNotificationsUseLabelsAndExcludeRemovedTrust(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	removedTrust := "AAAAAAAAAAAAAAAAAAAAAA"
	remainingTrust := "AgAAAAAAAAAAAAAAAAAAAA"
	authority := &selectiveTrustAuthority{active: map[string]bool{removedTrust: true, remainingTrust: true}}
	var logs strings.Builder
	manager := NewManager(store, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := manager.EnableProtected(authority, map[string]struct{}{removedTrust: {}, remainingTrust: {}}); err != nil {
		t.Fatal(err)
	}
	for endpoint, trustID := range map[string]string{
		"https://push.example/removed":   removedTrust,
		"https://push.example/remaining": remainingTrust,
	} {
		subscription := validSubscription(t, endpoint, DefaultEventSettings())
		if err := manager.CommitOwned(subscription, trustID); err != nil {
			t.Fatal(err)
		}
	}
	sender := &captureSender{}
	manager.sender = sender
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	manager.TrustedSignInAdded("  Tablet <script>  ")
	manager.WaitPending()
	added := sender.take()
	if len(added) != 2 {
		t.Fatalf("add sends=%d, want both active subscriptions", len(added))
	}
	for _, sent := range added {
		if sent.event.Kind != EventTrustedSignInAdded || sent.event.TrustLabel != "Tablet <script>" || sent.event.Destination != "/" {
			t.Fatalf("added event=%+v", sent.event)
		}
	}

	authority.setActive(removedTrust, false)
	if err := manager.RemoveTrustSubscriptions(removedTrust, "Phone <b>"); err != nil {
		t.Fatal(err)
	}
	manager.WaitPending()
	if _, found, err := store.LookupOwned("https://push.example/removed", removedTrust); err != nil || found {
		t.Fatalf("removed subscription found=%t err=%v", found, err)
	}
	removed := sender.take()
	if len(removed) != 1 || removed[0].trustID != remainingTrust || removed[0].event.Kind != EventTrustedSignInRemoved ||
		removed[0].event.TrustLabel != "Phone <b>" || removed[0].event.Destination != "/" {
		t.Fatalf("removed sends=%+v", removed)
	}
	logged := logs.String()
	for _, report := range []string{"Trusted sign-in added", "Trusted sign-in removed", "Tablet <script>", "Phone <b>"} {
		if !strings.Contains(logged, report) {
			t.Errorf("running-service report omitted %q: %s", report, logged)
		}
	}
	for _, secret := range []string{removedTrust, remainingTrust, "https://push.example/removed", "https://push.example/remaining"} {
		if strings.Contains(logged, secret) {
			t.Errorf("running-service report exposed %q: %s", secret, logged)
		}
	}
}

type gatedTrustAuthority struct {
	mu     sync.RWMutex
	active bool
}

func (a *gatedTrustAuthority) WithActiveTrustRead(_ string, operation func() error) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.active {
		return errors.New("inactive")
	}
	return operation()
}

func (a *gatedTrustAuthority) revoke() {
	a.mu.Lock()
	a.active = false
	a.mu.Unlock()
}

type blockingSender struct {
	calls   int
	release chan struct{}
	started chan struct{}
}

func (s *blockingSender) Send(context.Context, Event, Subscription, string, string, string) sendOutcome {
	s.calls++
	close(s.started)
	<-s.release
	return sendAccepted
}

func TestProtectedDeliveryRechecksOwnerAtTheSendCutoff(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	trustID := "AAAAAAAAAAAAAAAAAAAAAA"
	authority := &testTrustAuthority{active: true}
	manager := NewManager(store, nil)
	if err := manager.EnableProtected(authority, map[string]struct{}{trustID: {}}); err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/protected", EventSettings{Blocked: true})
	if err := manager.CommitOwned(subscription, trustID); err != nil {
		t.Fatal(err)
	}
	sender := &countingSender{}
	manager.sender = sender
	event := Event{Kind: EventStatus, Status: herdr.StatusBlocked}
	manager.deliverEvent(context.Background(), event)
	if sender.calls != 1 || authority.calls != 1 {
		t.Fatalf("active delivery sends=%d authority_checks=%d", sender.calls, authority.calls)
	}
	authority.active = false
	manager.deliverEvent(context.Background(), event)
	if sender.calls != 1 || authority.calls != 2 {
		t.Fatalf("inactive delivery sends=%d authority_checks=%d", sender.calls, authority.calls)
	}
}

func TestProtectedRevocationWaitsForPreCutoffPushAndPreventsANewSend(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	trustID := "AAAAAAAAAAAAAAAAAAAAAA"
	authority := &gatedTrustAuthority{active: true}
	manager := NewManager(store, nil)
	if err := manager.EnableProtected(authority, map[string]struct{}{trustID: {}}); err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/cutoff", EventSettings{Blocked: true})
	if err := manager.CommitOwned(subscription, trustID); err != nil {
		t.Fatal(err)
	}
	sender := &blockingSender{release: make(chan struct{}), started: make(chan struct{})}
	manager.sender = sender
	event := Event{Kind: EventStatus, Status: herdr.StatusBlocked}
	delivered := make(chan struct{})
	go func() {
		manager.deliverEvent(context.Background(), event)
		close(delivered)
	}()
	select {
	case <-sender.started:
	case <-time.After(time.Second):
		t.Fatal("pre-cutoff push did not start")
	}
	revoked := make(chan struct{})
	go func() {
		authority.revoke()
		close(revoked)
	}()
	select {
	case <-revoked:
		t.Fatal("revocation crossed an in-flight push read lease")
	case <-time.After(25 * time.Millisecond):
	}
	close(sender.release)
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("pre-cutoff push did not finish")
	}
	select {
	case <-revoked:
	case <-time.After(time.Second):
		t.Fatal("revocation did not complete after the push")
	}
	manager.deliverEvent(context.Background(), event)
	if sender.calls != 1 {
		t.Fatalf("post-cutoff delivery began %d sends, want 1 total", sender.calls)
	}
}

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
