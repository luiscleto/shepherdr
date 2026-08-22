package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/luisc/shepherdr/internal/herdr"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestWebPushSendIsSingleBoundedAttemptWithFiveMinuteTTL(t *testing.T) {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/send", EventSettings{Blocked: true})
	event := Event{
		Kind: EventStatus, Status: herdr.StatusBlocked, PaneID: "w1:p1", TerminalID: "term-1",
		Destination: "/#terminal=w1%3Ap1&terminal_id=term-1",
	}
	attempts := 0
	sender := &webPushSender{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.Header.Get("TTL") != "300" {
			t.Fatalf("TTL = %q, want 300", request.Header.Get("TTL"))
		}
		if request.ContentLength <= 0 || request.ContentLength > maxPushRequestBytes {
			t.Fatalf("encrypted request length = %d", request.ContentLength)
		}
		if subject := vapidSubject(t, request.Header.Get("Authorization")); subject != "mailto:operator@example.com" {
			t.Fatalf("VAPID subject = %q, want configured mailto contact", subject)
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("accepted")), Header: make(http.Header)}, nil
	})}}
	if outcome := sender.Send(context.Background(), event, subscription, "mailto:operator@example.com", publicKey, privateKey); outcome != sendAccepted {
		t.Fatalf("send outcome = %v, want accepted", outcome)
	}
	if attempts != 1 {
		t.Fatalf("send made %d attempts, want 1", attempts)
	}

	attempts = 0
	sender.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if subject := vapidSubject(t, request.Header.Get("Authorization")); subject != "https://operator.example" {
			t.Fatalf("VAPID subject = %q, want configured HTTPS contact", subject)
		}
		return nil, errors.New("ambiguous timeout")
	})
	if outcome := sender.Send(context.Background(), event, subscription, "https://operator.example", publicKey, privateKey); outcome != sendFailed || attempts != 1 {
		t.Fatalf("ambiguous send outcome=%v attempts=%d", outcome, attempts)
	}
}

func TestOutboundTransportCannotMultiplexOrReusePushRequests(t *testing.T) {
	sender := newWebPushSender(newEndpointValidator(fixedResolver{}))
	transport, ok := sender.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("outbound transport type = %T", sender.client.Transport)
	}
	if transport.ForceAttemptHTTP2 || transport.TLSNextProto == nil || !transport.DisableKeepAlives {
		t.Fatalf(
			"transport replay controls: force_http2=%t tls_next_proto_nil=%t disable_keep_alives=%t",
			transport.ForceAttemptHTTP2,
			transport.TLSNextProto == nil,
			transport.DisableKeepAlives,
		)
	}
}

func vapidSubject(t *testing.T, authorization string) string {
	t.Helper()
	token := strings.TrimPrefix(strings.SplitN(authorization, ",", 2)[0], "vapid t=")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid VAPID authorization header %q", authorization)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	subject, _ := claims["sub"].(string)
	return subject
}

func TestPayloadsCarryOnlyWorkspaceDisplayNameAndExactDestinationFields(t *testing.T) {
	payload, err := eventPayload(Event{
		Kind: EventStatus, Status: herdr.StatusDone, PaneID: "w1:p1", TerminalID: "term-1",
		WorkspaceName: "Review workspace", Destination: "/#terminal=w1%3Ap1&terminal_id=term-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(payload)
	want := `{"destination":"/#terminal=w1%3Ap1\u0026terminal_id=term-1","kind":"status","workspace_name":"Review workspace","pane_id":"w1:p1","status":"done","terminal_id":"term-1"}`
	if got != want {
		t.Fatalf("status payload = %s", got)
	}
	for _, forbidden := range []string{"repository", "checkout", "path", "agent", "message", "body", "title"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("payload exposed %q: %s", forbidden, got)
		}
	}
	workspace, _ := eventPayload(Event{Kind: EventWorkspaceClosed, WorkspaceName: "Temporary", Destination: "/"})
	if string(workspace) != `{"destination":"/","kind":"workspace_closed","workspace_name":"Temporary"}` {
		t.Fatalf("workspace payload = %s", workspace)
	}
	accessChange, _ := eventPayload(Event{Kind: EventTrustedSignInRemoved, TrustLabel: "Phone <script>", Destination: "/"})
	if string(accessChange) != `{"destination":"/","kind":"trusted_sign_in_removed","trust_label":"Phone \u003cscript\u003e"}` {
		t.Fatalf("access-change payload = %s", accessChange)
	}
	if libraryContact("mailto:operator@example.com") != "operator@example.com" {
		t.Fatal("mailto contact was not adapted to the pinned library without changing its value")
	}
}

type fixedSender struct{ outcome sendOutcome }

func (s fixedSender) Send(context.Context, Event, Subscription, string, string, string) sendOutcome {
	return s.outcome
}

func TestManagerRemovesSubscriptionOnlyAfterDefinitiveGone(t *testing.T) {
	path := t.TempDir() + "/notifications.json"
	store, err := OpenStore(path, "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/send", EventSettings{Blocked: true})
	if err := store.Upsert(subscription); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, nil)
	manager.sender = fixedSender{outcome: sendFailed}
	manager.deliverEvent(context.Background(), Event{Kind: EventStatus, Status: herdr.StatusBlocked})
	if _, found, _ := store.Lookup(subscription.Endpoint); !found {
		t.Fatal("ambiguous failure removed the subscription")
	}
	manager.sender = fixedSender{outcome: sendGone}
	manager.deliverEvent(context.Background(), Event{Kind: EventStatus, Status: herdr.StatusBlocked})
	if _, found, _ := store.Lookup(subscription.Endpoint); found {
		t.Fatal("definitive gone response kept the expired subscription")
	}
}
