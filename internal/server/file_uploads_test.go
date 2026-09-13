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

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/uploads"
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

type trialInputWriter struct {
	bytes.Buffer
	write func([]byte) (int, error)
}

func (writer *trialInputWriter) Write(data []byte) (int, error) {
	if writer.write != nil {
		return writer.write(data)
	}
	return writer.Buffer.Write(data)
}
func (*trialInputWriter) Close() error { return nil }

func trialController(t *testing.T, request *http.Request, writer io.WriteCloser) (*TerminalBridge, *terminalLiveController) {
	t.Helper()
	source := newFakeTerminalStateSource(recognizedAgentState("workspace-1", "pane-1", "term-send"))
	bridge := testTerminalBridge(source)
	lease, ok := bridge.acquireTarget("pane-1", "term-send")
	if !ok {
		t.Fatal("target unavailable")
	}
	session := &terminalChildSession{stdin: writer, controlled: true, exited: make(chan struct{})}
	controller, err := bridge.registerController(request, session, lease)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bridge.unregisterController(controller); lease.Close(); bridge.Close() })
	return bridge, controller
}

func TestTrialControllerHandleRequiresExactLiveTargetAndSignIn(t *testing.T) {
	writer := &trialInputWriter{}
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", nil)
	identity := access.SessionIdentity{Digest: "session-a", TrustID: "trust-a"}
	request = request.WithContext(context.WithValue(request.Context(), sessionContextKey, identity))
	bridge, controller := trialController(t, request, writer)
	validate := func() error { return nil }
	wrongSignIn := request.WithContext(context.WithValue(request.Context(), sessionContextKey, access.SessionIdentity{Digest: "session-b", TrustID: "trust-a"}))
	for _, candidate := range []struct {
		request                *http.Request
		handle, pane, terminal string
	}{
		{request, "wrong", "pane-1", "term-send"},
		{request, controller.handle, "pane-2", "term-send"},
		{request, controller.handle, "pane-1", "term-other"},
		{wrongSignIn, controller.handle, "pane-1", "term-send"},
		{httptest.NewRequest(http.MethodPost, "/", nil), controller.handle, "pane-1", "term-send"},
	} {
		outcome, _ := bridge.sendControllerBatch(candidate.request, candidate.handle, candidate.pane, candidate.terminal, validate, []string{"input"})
		if outcome != terminalBatchNotSent || writer.Len() != 0 {
			t.Fatal("invalid handle forwarded input")
		}
	}
	bridge.unregisterController(controller)
	outcome, _ := bridge.sendControllerBatch(request, controller.handle, "pane-1", "term-send", validate, []string{"input"})
	if outcome != terminalBatchNotSent || writer.Len() != 0 {
		t.Fatal("stale handle forwarded input")
	}
}

func TestTrialInsertionHasOnePasteAndNoSubmission(t *testing.T) {
	chunks, err := uploadTerminalInsertion([]string{"/tmp/one.txt", "/tmp/two.png"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0] != "\x1b[200~[User uploaded /tmp/one.txt] [User uploaded /tmp/two.png] \x1b[201~" || strings.ContainsAny(chunks[0], "\r\n") {
		t.Fatalf("insertion = %q", chunks)
	}
	for _, path := range []string{"/tmp/bad\nname", "/tmp/bad\rname", "/tmp/bad\x1bname"} {
		if _, err := uploadTerminalInsertion([]string{path}); err == nil {
			t.Fatal("unsafe path accepted")
		}
	}
}

func TestTrialRetainedUploadStagesAndRollsBackOrRetainsUnknown(t *testing.T) {
	for _, mode := range []string{"forwarded", "released", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", nil)
			writer := &trialInputWriter{}
			bridge, controller := trialController(t, request, writer)
			if mode == "released" {
				controller.session.controlled = false
			}
			if mode == "unknown" {
				writer.write = func(data []byte) (int, error) { return 1, io.ErrClosedPipe }
			}
			root := t.TempDir()
			manager, err := uploads.Open(filepath.Join(root, "files"), filepath.Join(root, "state", "uploads.json"), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, bridge, false)
			application.ConfigureSignInOff()
			application.SetFileUploads(manager, uploads.Limit{DecodedBytes: 32})
			application.fileUploadState = bridge.targets
			body, _ := json.Marshal(map[string]any{
				"files":   []map[string]string{{"name": "trial.txt", "data": "AQID"}},
				"pane_id": "pane-1", "terminal_id": "term-send", "workspace_id": "workspace-1",
				"controller": controller.handle, "intent": "insert",
			})
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			application.Handler().ServeHTTP(response, request)
			var result terminalFileResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			want := map[string]uploads.ForwardResult{"forwarded": uploads.Forwarded, "released": uploads.NotSent, "unknown": uploads.Unknown}[mode]
			if result.Result != want {
				t.Fatalf("outcome = %s, want %s: %s", result.Result, want, response.Body.String())
			}
			staged, err := filepath.Glob(filepath.Join(root, "files", "*", "trial.txt"))
			if err != nil {
				t.Fatal(err)
			}
			wantFiles := 1
			if mode == "released" {
				wantFiles = 0
			}
			if len(staged) != wantFiles {
				t.Fatalf("retained files = %d, want %d", len(staged), wantFiles)
			}
			if mode == "forwarded" {
				var command struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(writer.Bytes(), &command); err != nil {
					t.Fatal(err)
				}
				path := strings.TrimSuffix(strings.TrimPrefix(command.Text, "\x1b[200~[User uploaded "), "] \x1b[201~")
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, []byte{1, 2, 3}) {
					t.Fatalf("staged bytes: %v, %v", data, err)
				}
				if strings.ContainsAny(command.Text, "\r\n") {
					t.Fatal("insertion submitted input")
				}
			}
		})
	}
}

func TestTrialReleaseAndInputSerializeWithCompleteUpload(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", nil)
	writer := &trialInputWriter{}
	bridge, controller := trialController(t, request, writer)
	started, resume, done := make(chan struct{}), make(chan struct{}), make(chan terminalBatchOutcome, 1)
	var once sync.Once
	writer.write = func(data []byte) (int, error) {
		once.Do(func() { close(started); <-resume })
		return writer.Buffer.Write(data)
	}
	go func() {
		result, _ := bridge.sendControllerBatch(request, controller.handle, "pane-1", "term-send", func() error { return nil }, []string{"paste", "\r"})
		done <- result
	}()
	<-started
	released := make(chan struct{})
	go func() {
		controller.session.writeMutex.Lock()
		_ = json.NewEncoder(controller.session.stdin).Encode(map[string]string{"type": "terminal.release"})
		controller.session.controlled = false
		controller.session.writeMutex.Unlock()
		close(released)
	}()
	close(resume)
	if result := <-done; result != terminalBatchForwarded {
		t.Fatal("complete upload failed")
	}
	<-released
	lines := strings.Split(strings.TrimSpace(writer.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "paste") || !strings.Contains(lines[1], `\r`) || !strings.Contains(lines[2], "terminal.release") {
		t.Fatalf("interleaved batch: %s", writer.String())
	}
	before := writer.Len()
	result, _ := bridge.sendControllerBatch(request, controller.handle, "pane-1", "term-send", func() error { return nil }, []string{"late"})
	if result != terminalBatchNotSent || writer.Len() != before {
		t.Fatal("released controller accepted input")
	}
}

func TestTrialUploadChecksStreamLifetimeBeforeEveryChunk(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", nil)
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	writer := &trialInputWriter{}
	bridge, controller := trialController(t, request.WithContext(ctx), writer)
	writes := 0
	writer.write = func(data []byte) (int, error) {
		writes++
		cancel()
		return len(data), nil
	}
	outcome, _ := bridge.sendControllerBatch(request, controller.handle, "pane-1", "term-send", func() error { return nil }, []string{"paste", "\r"})
	if outcome != terminalBatchUnknown || writes != 1 {
		t.Fatalf("cancelled stream: outcome=%d writes=%d", outcome, writes)
	}
	outcome, _ = bridge.sendControllerBatch(request, controller.handle, "pane-1", "term-send", func() error { return nil }, []string{"retry"})
	if outcome != terminalBatchNotSent || writes != 1 {
		t.Fatal("cancelled stream accepted another input")
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

type lateAgentValidationSource struct {
	*fakeTerminalStateSource
	calls   int
	callsMu sync.Mutex
}

func (source *lateAgentValidationSource) Current() herdr.State {
	source.callsMu.Lock()
	source.calls++
	call := source.calls
	source.callsMu.Unlock()
	state := source.fakeTerminalStateSource.Current()
	if call >= 4 {
		state.Snapshot.Agents = nil
	}
	return state
}

func TestTerminalFileRouteRevalidatesAgentAfterControlAndRollsBack(t *testing.T) {
	baseSource := newFakeTerminalStateSource(recognizedAgentState("workspace-1", "pane-1", "term-send"))
	source := &lateAgentValidationSource{fakeTerminalStateSource: baseSource}
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	record := filepath.Join(t.TempDir(), "terminal-input.jsonl")
	bridge.command = func(ctx context.Context, arguments ...string) *exec.Cmd {
		return terminalHelperCommand(ctx, append(arguments, "--record="+record)...)
	}
	uploadBase := t.TempDir()
	stateDirectory := filepath.Join(uploadBase, "state")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	filesRoot := filepath.Join(uploadBase, "files")
	manager, err := uploads.Open(filesRoot, filepath.Join(stateDirectory, "uploads.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, bridge, false)
	application.ConfigureSignInOff()
	application.SetFileUploads(manager, uploads.Limit{DecodedBytes: 32})
	application.fileUploadState = source

	request := httptest.NewRequest(http.MethodPost, "/api/terminal/files", bytes.NewReader(terminalFileBody(t, "", "late.txt", []byte("late"), false)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("late agent loss returned %d: %s", response.Code, response.Body.String())
	}
	var result terminalFileResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Result != uploads.NotSent {
		t.Fatalf("late agent loss response = %+v, %v", result, err)
	}
	recorded, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var command map[string]string
	if err := json.Unmarshal(recorded, &command); err != nil || command["type"] != "terminal.release" {
		t.Fatalf("commands after late agent loss = %q, %v", recorded, err)
	}
	directories, err := os.ReadDir(filesRoot)
	if err != nil || len(directories) != 1 || !directories[0].IsDir() {
		t.Fatalf("upload directories after request rollback = %+v, %v", directories, err)
	}
	entries, err := os.ReadDir(filepath.Join(filesRoot, directories[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".shepherdr-upload" {
		t.Fatalf("late validation left request files: %+v", entries)
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
	for name, path := range map[string]string{
		"control":             "/tmp/bad\npath",
		"format":              "/tmp/left\u202eright",
		"line separator":      "/tmp/left\u2028right",
		"paragraph separator": "/tmp/left\u2029right",
	} {
		if _, err := uploadTerminalSubmission([]string{path}, "text"); err == nil {
			t.Errorf("generated path with %s was accepted", name)
		}
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
