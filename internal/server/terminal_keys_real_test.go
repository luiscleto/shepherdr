package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

// Opt-in only: the caller must supply an owned disposable terminal recording
// input. The trust fixture issues test sessions; Herdr and handlers are real.
func TestTerminalKeyRealProtected(t *testing.T) {
	pane, terminal := os.Getenv("SHEPHERDR_KEYS_OWNED_PANE"), os.Getenv("SHEPHERDR_KEYS_OWNED_TERMINAL")
	if pane == "" || terminal == "" {
		t.Skip("requires an explicitly owned disposable Herdr target")
	}
	socket := os.Getenv("SHEPHERDR_KEYS_HERDR_SOCKET")
	if socket == "" {
		t.Fatal("explicit Herdr socket required")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := herdr.NewClient(socket, logger)
	projector := herdr.NewProjector(client)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { projector.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for projector.Current().Connection != herdr.ConnectionLive && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := terminalTargetFromState(projector.Current(), pane, terminal); !ok {
		t.Fatal("owned target is unavailable")
	}
	binary, err := exec.LookPath("herdr")
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewTerminalBridge(binary, socket, logger, projector)
	defer bridge.Close()
	protected := newProtectedTestServerWithOptions(t, bridge, protectedTestServerOptions{credentialCount: 2})
	defer protected.manager.Close()
	defer protected.store.Close()
	protected.application.projector = projector
	server := httptest.NewServer(protected.application.Handler())
	defer server.Close()
	path := "/api/terminal?pane=" + pane + "&terminal=" + terminal + "&mode=control&cols=80&rows=24"
	connection := dialProtectedWebSocket(t, server.URL, path, protected.tokens[0])
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message struct {
		Type      string
		Result    string
		RequestID uint64 `json:"request_id"`
	}
	for {
		if err := connection.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type == "terminal.frame" {
			break
		}
	}
	if err := connection.WriteJSON(map[string]any{"type": "terminal.send-key", "request_id": 1, "key": map[string]any{"base": "q", "super": true}}); err != nil {
		t.Fatal(err)
	}
	for {
		if err := connection.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type == "terminal.key-result" {
			break
		}
	}
	if message.Result != "accepted" || message.RequestID != 1 {
		t.Fatalf("key outcome %+v", message)
	}
	waitForTerminalChildren(t, bridge, 1)
	request, _ := http.NewRequest("POST", server.URL+"/api/auth/sign-out", bytes.NewBufferString("{}"))
	request.Host = "shepherdr.private"
	request.Header.Set("Origin", "https://shepherdr.private")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: protected.tokens[0]})
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("sign-out status %d", response.StatusCode)
	}
	waitForTerminalChildren(t, bridge, 0)
	_ = connection.WriteJSON(map[string]any{"type": "terminal.send-key", "request_id": 2, "key": map[string]any{"base": "x"}})
	for i := 0; i < 20; i++ {
		if err := connection.ReadJSON(&message); err != nil {
			break
		}
		if message.Type == "terminal.key-result" && message.RequestID == 2 && message.Result == "accepted" {
			t.Fatal("input accepted after sign-out")
		}
		if i == 19 {
			t.Fatal("socket did not close")
		}
	}
	request, _ = http.NewRequest("GET", server.URL+path, nil)
	request.Host = "shepherdr.private"
	request.Header.Set("Origin", "https://shepherdr.private")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: protected.tokens[0]})
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed-out target request status %d", response.StatusCode)
	}
	t.Log("one accepted key retained the controller; production sign-out closed the child/socket and rejected the old session")
}
