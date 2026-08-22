package notifications

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	PushTTL             = 5 * time.Minute
	pushRequestTimeout  = 8 * time.Second
	maxPushRequestBytes = 8 << 10
	maxPushResponseBody = 4 << 10
	maxPayloadBytes     = 2 << 10
)

type sendOutcome uint8

const (
	sendFailed sendOutcome = iota
	sendAccepted
	sendGone
)

type deliverySender interface {
	Send(context.Context, Event, Subscription, string, string, string) sendOutcome
}

type webPushSender struct {
	client *http.Client
}

func newWebPushSender(validator *endpointValidator) *webPushSender {
	dialer := &safeDialer{
		validator: validator,
		dialer:    netDialer(),
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      false,
		TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 16 << 10,
		DisableCompression:     true,
	}
	return &webPushSender{client: &http.Client{
		Transport: transport,
		Timeout:   pushRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func netDialer() *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second}
}

func (s *webPushSender) Send(ctx context.Context, event Event, subscription Subscription, contact, publicKey, privateKey string) sendOutcome {
	payload, err := eventPayload(event)
	if err != nil || len(payload) > maxPayloadBytes {
		return sendFailed
	}
	requestCtx, cancel := context.WithTimeout(ctx, pushRequestTimeout)
	defer cancel()
	client := boundedHTTPClient{s.client}
	response, err := webpush.SendNotificationWithContext(requestCtx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			Auth:   subscription.Keys.Auth,
			P256dh: subscription.Keys.P256dh,
		},
	}, &webpush.Options{
		HTTPClient:      client,
		Subscriber:      libraryContact(contact),
		TTL:             int(PushTTL.Seconds()),
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
	})
	if err != nil {
		return sendFailed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPushResponseBody+1))
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return sendGone
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return sendAccepted
	}
	return sendFailed
}

type boundedHTTPClient struct {
	client *http.Client
}

func (c boundedHTTPClient) Do(request *http.Request) (*http.Response, error) {
	if request.ContentLength < 0 || request.ContentLength > maxPushRequestBytes {
		return nil, errors.New("push request is too large")
	}
	return c.client.Do(request)
}

type pushPayload struct {
	Destination   string `json:"destination"`
	Kind          string `json:"kind"`
	WorkspaceName string `json:"workspace_name,omitempty"`
	PaneID        string `json:"pane_id,omitempty"`
	Status        string `json:"status,omitempty"`
	TerminalID    string `json:"terminal_id,omitempty"`
}

func eventPayload(event Event) ([]byte, error) {
	payload := pushPayload{
		Destination: event.Destination, Kind: string(event.Kind), WorkspaceName: event.WorkspaceName,
	}
	if event.Kind == EventStatus {
		payload.PaneID = event.PaneID
		payload.Status = string(event.Status)
		payload.TerminalID = event.TerminalID
	}
	return json.Marshal(payload)
}

func libraryContact(contact string) string {
	return strings.TrimPrefix(contact, "mailto:")
}
