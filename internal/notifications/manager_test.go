package notifications

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
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
	manager := NewManager(store, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
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
	manager.TrustedSignInAdded("  ")
	manager.WaitPending()
	if generic := sender.take(); len(generic) != 1 || generic[0].event.TrustLabel != "" {
		t.Fatalf("generic-label sends=%+v", generic)
	}
	logged := logs.String()
	for _, entry := range []struct {
		message string
		fields  []string
	}{
		{"Trusted sign-in added", []string{"event_kind=trusted_sign_in_added", `trusted_sign_in_label="Tablet <script>"`}},
		{"Notification candidate enqueued", []string{"event_kind=trusted_sign_in_added", `trusted_sign_in_label="Tablet <script>"`}},
		{"Push service accepted notification", []string{"event_kind=trusted_sign_in_added", `trusted_sign_in_label="Tablet <script>"`}},
		{"Trusted sign-in removed", []string{"event_kind=trusted_sign_in_removed", `trusted_sign_in_label="Phone <b>"`}},
		{"Notification candidate enqueued", []string{"event_kind=trusted_sign_in_removed", `trusted_sign_in_label="Phone <b>"`}},
		{"Push service accepted notification", []string{"event_kind=trusted_sign_in_removed", `trusted_sign_in_label="Phone <b>"`}},
		{"Trusted sign-in added", []string{"event_kind=trusted_sign_in_added", `trusted_sign_in_label="Trusted sign-in"`}},
	} {
		requireLogEntry(t, logged, entry.message, entry.fields...)
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

func TestNotificationLifecycleDiagnosticsRespectLogLevel(t *testing.T) {
	for _, test := range []struct {
		name  string
		level slog.Leveler
		want  bool
	}{
		{name: "default info"},
		{name: "debug", level: slog.LevelDebug, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
			if err != nil {
				t.Fatal(err)
			}
			subscription := validSubscription(t, "https://push.example/debug-level", EventSettings{Blocked: true})
			if err := store.Upsert(subscription); err != nil {
				t.Fatal(err)
			}
			var logs strings.Builder
			options := &slog.HandlerOptions{Level: test.level}
			manager := NewManager(store, slog.New(slog.NewTextHandler(&logs, options)))
			manager.sender = fixedSender{outcome: sendAccepted}

			manager.ObserveSnapshot(notificationSnapshot("working", "w1"), true)
			manager.ObserveSnapshot(notificationSnapshot("blocked", "w1"), false)
			event := <-manager.events
			manager.deliverEvent(context.Background(), event)
			event.pending.Done()

			got := logs.String()
			for _, message := range []string{
				"Notification candidate observed",
				"Notification candidate enqueued",
				"Push service accepted notification",
			} {
				if present := strings.Contains(got, `msg="`+message+`"`); present != test.want {
					t.Errorf("log message %q present = %t, want %t: %s", message, present, test.want, got)
				}
				if test.want {
					requireLogEntry(t, got, message, "level=DEBUG", "event_kind=status", `workspace_name="Workspace w1"`, "herdr_status=blocked")
				}
			}
		})
	}
}

func TestDeliveryDiagnosticsReportStatusFieldsWithoutPrivateData(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/private-endpoint", EventSettings{Blocked: true})
	if err := store.Upsert(subscription); err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	manager := NewManager(store, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

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
	for _, entry := range []struct {
		message string
		fields  []string
	}{
		{"Notification candidate observed", []string{"event_kind=status", `workspace_name="Workspace w1"`, "herdr_status=blocked"}},
		{"Notification candidate enqueued", []string{"event_kind=status", `workspace_name="Workspace w1"`, "herdr_status=blocked"}},
		{"Notification candidate dropped", []string{"event_kind=status", `workspace_name="Workspace w1"`, "herdr_status=done"}},
		{"Push service accepted notification", []string{"event_kind=status", `workspace_name="Private workspace"`, "herdr_status=blocked"}},
		{"Notification push failed", []string{"event_kind=status", `workspace_name="Private workspace"`, "herdr_status=blocked"}},
		{"Push service reports subscription gone", []string{"event_kind=status", `workspace_name="Private workspace"`, "herdr_status=blocked"}},
	} {
		requireLogEntry(t, got, entry.message, entry.fields...)
	}
	for _, private := range []string{
		"w1:p1", "term-1", "private-pane", "private-terminal",
		"private-destination", subscription.Endpoint, subscription.Keys.Auth, subscription.Keys.P256dh,
	} {
		if strings.Contains(got, private) {
			t.Errorf("diagnostics exposed private value %q: %s", private, got)
		}
	}
}

func TestDeliveryDiagnosticsReportWorkspaceFieldsAndEmptyBurstName(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	manager := NewManager(store, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	base := herdr.WorkspaceInfo{WorkspaceID: "secret-base", Label: "Base"}
	temporary := herdr.WorkspaceInfo{WorkspaceID: "secret-temporary", Label: "Temporary <script>"}
	one := herdr.WorkspaceInfo{WorkspaceID: "secret-one", Label: "One"}
	two := herdr.WorkspaceInfo{WorkspaceID: "secret-two", Label: "Two"}

	manager.ObserveSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{base}}, true)
	manager.ObserveSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{base, temporary}}, false)
	manager.ObserveSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{base}}, false)
	manager.ObserveSnapshot(herdr.Snapshot{Workspaces: []herdr.WorkspaceInfo{base, one, two}}, false)

	got := logs.String()
	for _, entry := range []struct {
		message string
		fields  []string
	}{
		{"Notification candidate observed", []string{"event_kind=workspace_opened", `workspace_name="Temporary <script>"`}},
		{"Notification candidate enqueued", []string{"event_kind=workspace_opened", `workspace_name="Temporary <script>"`}},
		{"Notification candidate observed", []string{"event_kind=workspace_closed", `workspace_name="Temporary <script>"`}},
		{"Notification candidate enqueued", []string{"event_kind=workspace_closed", `workspace_name="Temporary <script>"`}},
		{"Notification candidate observed", []string{"event_kind=workspace_opened", `workspace_name=""`}},
	} {
		requireLogEntry(t, got, entry.message, entry.fields...)
	}
	for _, secret := range []string{"secret-base", "secret-temporary", "secret-one", "secret-two"} {
		if strings.Contains(got, secret) {
			t.Errorf("workspace diagnostics exposed identifier %q: %s", secret, got)
		}
	}
}

func requireLogEntry(t *testing.T, logs, message string, fields ...string) {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, `msg="`+message+`"`) {
			continue
		}
		matched := true
		for _, field := range fields {
			if !strings.Contains(line, field) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Errorf("diagnostics omitted message %q with fields %v: %s", message, fields, logs)
}
