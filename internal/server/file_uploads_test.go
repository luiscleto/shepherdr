package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/luisc/shepherdr/internal/access"
	"github.com/luisc/shepherdr/internal/herdr"
	"github.com/luisc/shepherdr/internal/uploads"
)

func recognizedAgentState(workspace, pane, terminal string) herdr.State {
	kind := "codex"
	return herdr.State{
		Connection: herdr.ConnectionLive,
		HasHome:    true,
		Home:       herdr.Home{Workspaces: []herdr.Workspace{}},
		Snapshot: herdr.Snapshot{
			Workspaces: []herdr.WorkspaceInfo{{ActiveTabID: "tab-1", Label: "Review", WorkspaceID: workspace}},
			Tabs:       []herdr.TabInfo{{TabID: "tab-1", WorkspaceID: workspace}},
			Panes:      []herdr.PaneInfo{{PaneID: pane, TabID: "tab-1", TerminalID: terminal, WorkspaceID: workspace}},
			Agents: []herdr.AgentInfo{{
				Agent: &kind, AgentStatus: herdr.StatusWorking, PaneID: pane, TabID: "tab-1", TerminalID: terminal, WorkspaceID: workspace,
			}},
			Layouts: []herdr.LayoutInfo{{
				TabID: "tab-1", WorkspaceID: workspace,
				Panes: []herdr.LayoutPane{{PaneID: pane, Rect: herdr.Rectangle{Width: 80, Height: 24}}},
			}},
		},
	}
}

func testUploadManager(t *testing.T) *uploads.Manager {
	t.Helper()
	base := t.TempDir()
	stateDirectory := filepath.Join(base, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := uploads.Open(filepath.Join(base, "files"), filepath.Join(stateDirectory, "uploads.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func terminalFileBody(t *testing.T, text, name string, data []byte, takeover bool) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"files":        []map[string]string{{"data": base64.StdEncoding.EncodeToString(data), "name": name}},
		"pane_id":      "pane-1",
		"takeover":     takeover,
		"terminal_id":  "term-send",
		"text":         text,
		"workspace_id": "workspace-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTerminalFileRouteStagesOpaqueBytesAndSendsOneServerDerivedBatch(t *testing.T) {
	source := newFakeTerminalStateSource(recognizedAgentState("workspace-1", "pane-1", "term-send"))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	record := filepath.Join(t.TempDir(), "terminal-input.jsonl")
	bridge.command = func(ctx context.Context, arguments ...string) *exec.Cmd {
		return terminalHelperCommand(ctx, append(arguments, "--record="+record)...)
	}
	manager := testUploadManager(t)
	application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, bridge, false)
	application.ConfigureSignInOff()
	application.SetFileUploads(manager, uploads.Limit{DecodedBytes: 32})
	application.fileUploadState = source

	body := terminalFileBody(t, "Please inspect", "../notes.txt", []byte{0, 1, 2, 255}, false)
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("file send returned %d: %s", response.Code, response.Body.String())
	}
	var result terminalFileResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Result != uploads.Forwarded {
		t.Fatalf("response = %+v, %v", result, err)
	}
	recorded, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(recorded), "\n")
	if len(lines) != 3 {
		t.Fatalf("terminal command lines = %d, want input, Enter, and release: %q", len(lines), recorded)
	}
	var paste struct {
		Text string `json:"text"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &paste); err != nil || paste.Type != "terminal.input" {
		t.Fatalf("paste command = %+v, %v", paste, err)
	}
	if !strings.HasPrefix(paste.Text, "\x1b[200~User uploaded files:\r- ") || !strings.HasSuffix(paste.Text, "\r\rPlease inspect\x1b[201~") {
		t.Fatalf("server batch text = %q", paste.Text)
	}
	path := strings.TrimSuffix(strings.TrimPrefix(paste.Text, "\x1b[200~User uploaded files:\r- "), "\r\rPlease inspect\x1b[201~")
	if filepath.Base(path) != "notes.txt" || !filepath.IsAbs(path) {
		t.Fatalf("generated path = %q", path)
	}
	staged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(staged, []byte{0, 1, 2, 255}) {
		t.Fatalf("staged bytes = %v, %v", staged, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("staged mode = %v, %v", infoMode(info), err)
	}
	var enter map[string]string
	if err := json.Unmarshal([]byte(lines[1]), &enter); err != nil || enter["text"] != "\r" {
		t.Fatalf("Enter command = %+v, %v", enter, err)
	}
}

func TestTerminalFileRequestEnforcesFiniteDecodedAndCanonicalBase64ButNoneHasNoRouteBound(t *testing.T) {
	finite := uploads.Limit{DecodedBytes: 3}
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", bytes.NewReader(terminalFileBody(t, "", "four", []byte("four"), false)))
	if _, _, err := readTerminalFileRequest(request, finite); !errors.Is(err, errUploadTooLarge) {
		t.Fatalf("decoded overage = %v", err)
	}
	unlimited := uploads.Limit{Unlimited: true}
	request = httptest.NewRequest(http.MethodPost, "/api/terminal/files", bytes.NewReader(terminalFileBody(t, "", "four", []byte("four"), false)))
	if _, files, err := readTerminalFileRequest(request, unlimited); err != nil || string(files[0].Data) != "four" {
		t.Fatalf("none request = %+v, %v", files, err)
	}
	body := strings.Replace(string(terminalFileBody(t, "", "one", []byte("one"), false)), base64.StdEncoding.EncodeToString([]byte("one")), "b25l\\n", 1)
	request = httptest.NewRequest(http.MethodPost, "/api/terminal/files", strings.NewReader(body))
	if _, _, err := readTerminalFileRequest(request, unlimited); err == nil {
		t.Fatal("noncanonical base64 was accepted")
	}
}

func TestUploadPrefixRejectsUnsafeGeneratedPathsAndNormalizesPersonText(t *testing.T) {
	if _, err := uploadTerminalSubmission([]string{"/tmp/bad\npath"}, "text"); err == nil {
		t.Fatal("generated path with a line break was accepted")
	}
	chunks, err := uploadTerminalSubmission([]string{"/tmp/diagram (1).svg"}, "one\n\x1b[200~two")
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[200~User uploaded files:\r- /tmp/diagram (1).svg\r\rone\r[200~two\x1b[201~"
	if chunks[0] != want || chunks[1] != "\r" {
		t.Fatalf("terminal upload chunks = %q", chunks)
	}
}

func TestTerminalFileRouteRequiresRecognizedAgentAndExactJSONInSignInOff(t *testing.T) {
	source := newFakeTerminalStateSource(recognizedAgentState("workspace-1", "pane-1", "term-send"))
	state := source.Current()
	state.Snapshot.Agents = nil
	source.publish(state)
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, bridge, false)
	application.ConfigureSignInOff()
	application.SetFileUploads(testUploadManager(t), uploads.Limit{DecodedBytes: 32})
	application.fileUploadState = source
	body := terminalFileBody(t, "", "one", []byte("one"), false)
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("ordinary terminal returned %d: %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/terminal/files", bytes.NewReader(body))
	response = httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing JSON content type returned %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "http://localhost/api/terminal/files", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://other.example")
	response = httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin sign-in-off upload returned %d", response.Code)
	}
}

func TestProtectedTerminalFileRouteRequiresSessionAndUsesBoundLease(t *testing.T) {
	source := newFakeTerminalStateSource(recognizedAgentState("workspace-1", "pane-1", "term-send"))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	application, accessManager, store, token := newProtectedTestServer(t, bridge)
	defer accessManager.Close()
	defer store.Close()
	application.SetFileUploads(testUploadManager(t), uploads.Limit{DecodedBytes: 32})
	application.fileUploadState = source
	body := terminalFileBody(t, "", "one", []byte("one"), false)

	request := httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/terminal/files", bytes.NewReader(body))
	request.Header.Set("Origin", "https://shepherdr.private")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unbound protected upload returned %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/terminal/files", bytes.NewReader(body))
	request.Header.Set("Origin", "https://shepherdr.private")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response = httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bound protected upload returned %d: %s", response.Code, response.Body.String())
	}
}

type blockingUploadBody struct {
	closeOnce sync.Once
	readOnce  sync.Once
	closed    chan struct{}
	started   chan struct{}
}

func newBlockingUploadBody() *blockingUploadBody {
	return &blockingUploadBody{closed: make(chan struct{}), started: make(chan struct{})}
}

func (body *blockingUploadBody) Read([]byte) (int, error) {
	body.readOnce.Do(func() { close(body.started) })
	<-body.closed
	return 0, context.Canceled
}

func (body *blockingUploadBody) Close() error {
	body.closeOnce.Do(func() { close(body.closed) })
	return nil
}

func TestProtectedTerminalFileInvalidationClosesBodyAndReleasesLease(t *testing.T) {
	source := newFakeTerminalStateSource(recognizedAgentState("workspace-1", "pane-1", "term-send"))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	application, accessManager, store, token := newProtectedTestServer(t, bridge)
	defer accessManager.Close()
	defer store.Close()
	application.SetFileUploads(testUploadManager(t), uploads.Limit{DecodedBytes: 32})
	application.fileUploadState = source
	body := newBlockingUploadBody()
	request := httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/terminal/files", body)
	request.Header.Set("Origin", "https://shepherdr.private")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		application.Handler().ServeHTTP(response, request)
		close(finished)
	}()
	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("upload handler did not begin reading its request body")
	}
	identity, ok := accessManager.AuthenticateToken(token)
	if !ok {
		t.Fatal("test session was not active")
	}
	runtimes, err := accessManager.SignOut(identity)
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan struct{})
	go func() {
		access.WaitRuntimes(runtimes)
		close(waited)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("session invalidation did not stop the upload body read")
	}
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("session invalidation did not wait for the upload lease to be released")
	}
	if response.Code == http.StatusOK {
		t.Fatalf("invalidated upload returned success: %s", response.Body.String())
	}
	waitForTerminalChildren(t, bridge, 0)
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}
