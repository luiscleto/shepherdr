package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gorilla/websocket"
	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestInvitationQRCodeIsAnIntrinsicSquarePNG(t *testing.T) {
	value, err := invitationQRDataURL("https://shepherdr.private/#trust=" + strings.Repeat("A", 43))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(value, prefix) {
		t.Fatalf("QR data URL prefix = %q", value[:min(len(value), len(prefix))])
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != config.Height || config.Width < 200 {
		t.Fatalf("QR dimensions = %dx%d", config.Width, config.Height)
	}
}

func TestProtectedRouteInventoryIsDenyByDefault(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
		want   protectedRoute
	}{
		{http.MethodGet, "/healthz", routePublic},
		{http.MethodGet, "/", routePublic},
		{http.MethodHead, "/app.js", routePublic},
		{http.MethodGet, "/api/home", routeWebSocket},
		{http.MethodGet, "/api/terminal", routeWebSocket},
		{http.MethodGet, "/api/terminal-lab", routeWebSocket},
		{http.MethodGet, "/api/terminal/read", routeProtected},
		{http.MethodGet, "/api/terminal-lab/read", routeProtected},
		{http.MethodGet, "/api/devices", routeProtected},
		{http.MethodPost, "/api/auth/sign-in/begin", routePublic},
		{http.MethodPost, "/api/auth/sign-in/finish", routePublic},
		{http.MethodPost, "/api/auth/trust/begin", routePublic},
		{http.MethodPost, "/api/auth/trust/finish", routePublic},
		{http.MethodPost, "/api/auth/reauthenticate/begin", routeProtected},
		{http.MethodPost, "/api/auth/reauthenticate/finish", routeProtected},
		{http.MethodPost, "/api/auth/sign-out", routeProtected},
		{http.MethodPost, "/api/devices/invitations", routeProtected},
		{http.MethodPost, "/api/devices/revoke", routeProtected},
		{http.MethodPost, "/api/workspace-actions/prepare", routeProtected},
		{http.MethodPost, "/api/workspace-actions", routeProtected},
		{http.MethodPost, "/api/terminal-actions/prepare", routeProtected},
		{http.MethodPost, "/api/terminal-actions", routeProtected},
		{http.MethodPost, "/api/notifications/config", routeProtected},
		{http.MethodPost, "/api/notifications/settings/read", routeProtected},
		{http.MethodPost, "/api/notifications/settings", routeProtected},
		{http.MethodPost, "/api/terminal/files", routeProtected},
		{http.MethodDelete, "/api/notifications/settings", routeProtected},
		{http.MethodGet, "/api", routeUnknown},
		{http.MethodGet, "/api/unknown", routeUnknown},
		{http.MethodGet, "/api/auth/sign-out", routeUnknown},
		{http.MethodPut, "/api/devices/revoke", routeUnknown},
	} {
		if got := classifyProtectedRoute(test.path, test.method); got != test.want {
			t.Errorf("%s %s classified %d, want %d", test.method, test.path, got, test.want)
		}
	}
}

func TestProtectedBoundaryEnforcesExactHostOriginJSONAndSession(t *testing.T) {
	application, manager, store, token := newProtectedTestServer(t, nil)
	defer manager.Close()
	defer store.Close()
	handler := application.Handler()

	request := httptest.NewRequest(http.MethodGet, "https://other.private/healthz", nil)
	request.Header.Set("X-Forwarded-Host", "shepherdr.private")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("forwarded host bypass returned %d", response.Code)
	}

	for _, path := range []string{"/api", "/api/unknown"} {
		response = serveProtected(handler, http.MethodGet, path, "", "", false)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s returned %d, want 404", path, response.Code)
		}
	}
	response = serveProtected(handler, http.MethodGet, "/api/devices", "", "", false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Devices returned %d", response.Code)
	}
	response = serveProtected(handler, http.MethodGet, "/api/devices", "", token, false)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated Devices returned %d: %s", response.Code, response.Body.String())
	}
	response = serveProtected(handler, http.MethodPost, "/api/devices/invitations", "https://shepherdr.private", "", true)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("self-authorized invitation returned %d", response.Code)
	}
	response = serveProtected(handler, http.MethodGet, "/api/auth/sign-out", "", token, false)
	if response.Code != http.StatusNotFound {
		t.Fatalf("state-changing auth GET returned %d", response.Code)
	}

	for name, configure := range map[string]func(*http.Request){
		"absent origin": func(*http.Request) {},
		"null origin":   func(request *http.Request) { request.Header.Set("Origin", "null") },
		"other port":    func(request *http.Request) { request.Header.Set("Origin", "https://shepherdr.private:8443") },
		"duplicate origin": func(request *http.Request) {
			request.Header.Add("Origin", "https://shepherdr.private")
			request.Header.Add("Origin", "https://shepherdr.private")
		},
		"parameterized json": func(request *http.Request) {
			request.Header.Set("Origin", "https://shepherdr.private")
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/auth/sign-in/begin", strings.NewReader("{}"))
			request.Header.Set("Content-Type", "application/json")
			configure(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	response = serveProtected(handler, http.MethodPost, "/api/auth/sign-in/begin", "https://shepherdr.private", "", true)
	if response.Code != http.StatusOK {
		t.Fatalf("exact public mutation returned %d: %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/auth/sign-in/begin", strings.NewReader(`{"padding":"`+strings.Repeat("x", maxAccessRequestBytes)+`"}`))
	request.Header.Set("Origin", "https://shepherdr.private")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized ceremony body returned %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "https://shepherdr.private/api/home", nil)
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Origin", "https://other.private")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin WebSocket returned %d", response.Code)
	}
}

func TestProtectedTerminalReadInvalidationCancelsAndCollectsChildBeforeSuccess(t *testing.T) {
	source := newFakeTerminalStateSource(liveTerminalState("pane-block", "term-1", 1))
	bridge := testTerminalBridge(source)
	defer bridge.Close()
	application, manager, store, token := newProtectedTestServer(t, bridge)
	defer manager.Close()
	defer store.Close()
	request := httptest.NewRequest(http.MethodGet, "https://shepherdr.private/api/terminal/read?pane=pane-block&terminal=term-1&lines=20&source=recent-unwrapped", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		application.Handler().ServeHTTP(response, request)
		close(finished)
	}()
	waitForTerminalChildren(t, bridge, 1)
	identity, ok := manager.AuthenticateToken(token)
	if !ok {
		t.Fatal("test session was not active")
	}
	runtimes, err := manager.SignOut(identity)
	if err != nil {
		t.Fatal(err)
	}
	access.WaitRuntimes(runtimes)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("protected Terminal read did not finish after invalidation")
	}
	waitForTerminalChildren(t, bridge, 0)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "replacement output must not escape") {
		t.Fatalf("invalidated read status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestProtectedTerminalLabReadInvalidationCancelsAndCollectsChildBeforeCommit(t *testing.T) {
	bridge := testTerminalBridge(nil)
	defer bridge.Close()
	testServer := newProtectedTestServerWithOptions(t, bridge, protectedTestServerOptions{terminalLab: true, credentialCount: 1})
	defer testServer.manager.Close()
	defer testServer.store.Close()
	request := httptest.NewRequest(http.MethodGet, "https://shepherdr.private/api/terminal-lab/read?pane=pane-block&lines=20&source=recent-unwrapped", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: testServer.tokens[0]})
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		testServer.application.Handler().ServeHTTP(response, request)
		close(finished)
	}()
	waitForTerminalChildren(t, bridge, 1)
	signOutTestSession(t, testServer.manager, testServer.tokens[0])
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("protected Terminal Lab read did not finish after invalidation")
	}
	waitForTerminalChildren(t, bridge, 0)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "replacement output must not escape") {
		t.Fatalf("invalidated Lab read status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestProtectedTerminalLabSocketsCloseAndCollectChildrenForEveryInvalidation(t *testing.T) {
	for _, name := range []string{"sign-out", "revoke", "reset", "expiry"} {
		t.Run(name, func(t *testing.T) {
			clock := &controlledAccessClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
			bridge := testTerminalBridge(nil)
			defer bridge.Close()
			testServer := newProtectedTestServerWithOptions(t, bridge, protectedTestServerOptions{
				terminalLab: true, credentialCount: 2, lifetime: "1d", clock: clock,
			})
			defer testServer.manager.Close()
			defer testServer.store.Close()
			httpServer := httptest.NewServer(testServer.application.Handler())
			defer httpServer.Close()
			connection := dialProtectedWebSocket(t, httpServer.URL, "/api/terminal-lab?pane=pane-1&mode=observe&cols=80&rows=24", testServer.tokens[0])
			defer connection.Close()
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := connection.ReadMessage(); err != nil {
				t.Fatalf("read initial Terminal Lab frame: %v", err)
			}
			waitForTerminalChildren(t, bridge, 1)
			switch name {
			case "sign-out":
				signOutTestSession(t, testServer.manager, testServer.tokens[0])
			case "revoke":
				result, err := testServer.manager.RevokeLocal(testServer.trustIDs[0])
				if err != nil {
					t.Fatal(err)
				}
				access.WaitRuntimes(result.Runtimes)
			case "reset":
				runtimes, err := testServer.manager.Reset()
				if err != nil {
					t.Fatal(err)
				}
				access.WaitRuntimes(runtimes)
			case "expiry":
				clock.Advance(24 * time.Hour)
			}
			waitForTerminalChildren(t, bridge, 0)
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := connection.ReadMessage(); err == nil {
				t.Fatalf("Terminal Lab socket remained open after %s", name)
			}
		})
	}
}

func TestProtectedInvalidationClosesHomeAndTerminalSocketsAndCollectsChildren(t *testing.T) {
	t.Run("Home", func(t *testing.T) {
		application, manager, store, token := newProtectedTestServer(t, nil)
		defer manager.Close()
		defer store.Close()
		application.projector = herdr.NewProjector(nil)
		httpServer := httptest.NewServer(application.Handler())
		defer httpServer.Close()
		connection := dialProtectedWebSocket(t, httpServer.URL, "/api/home", token)
		defer connection.Close()
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err := connection.ReadMessage(); err != nil {
			t.Fatalf("read initial Home frame: %v", err)
		}
		signOutTestSession(t, manager, token)
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err := connection.ReadMessage(); err == nil {
			t.Fatal("Home socket remained open after sign-out")
		}
	})

	for _, mode := range []string{"observe", "takeover"} {
		t.Run("Terminal "+mode, func(t *testing.T) {
			source := newFakeTerminalStateSource(liveTerminalState("pane-1", "term-1", 1))
			bridge := testTerminalBridge(source)
			defer bridge.Close()
			application, manager, store, token := newProtectedTestServer(t, bridge)
			defer manager.Close()
			defer store.Close()
			httpServer := httptest.NewServer(application.Handler())
			defer httpServer.Close()
			path := "/api/terminal?pane=pane-1&terminal=term-1&mode=" + mode + "&cols=80&rows=24"
			connection := dialProtectedWebSocket(t, httpServer.URL, path, token)
			defer connection.Close()
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := connection.ReadMessage(); err != nil {
				t.Fatalf("read initial Terminal frame: %v", err)
			}
			waitForTerminalChildren(t, bridge, 1)
			signOutTestSession(t, manager, token)
			waitForTerminalChildren(t, bridge, 0)
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := connection.ReadMessage(); err == nil {
				t.Fatal("Terminal socket remained open after sign-out")
			}
		})
	}
}

func TestAccessCookiesAreHostOnlyStrictAndUseIssuanceLifetime(t *testing.T) {
	created := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	expires := created.Add(365 * 24 * time.Hour)
	finite := httptest.NewRecorder()
	setSessionCookie(finite, access.SessionIssue{Token: "finite", CreatedAt: created, ExpiresAt: &expires, Lifetime: "365d"})
	cookies := finite.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Domain != "" || cookies[0].Path != "/" ||
		!cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != 365*24*60*60 || !cookies[0].Expires.Equal(expires) {
		t.Fatalf("finite session cookie = %+v", cookies)
	}
	browser := httptest.NewRecorder()
	setSessionCookie(browser, access.SessionIssue{Token: "browser", CreatedAt: created, Lifetime: "none"})
	cookies = browser.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != 0 || !cookies[0].Expires.IsZero() {
		t.Fatalf("browser session cookie = %+v", cookies)
	}
}

func TestCeremonyFinishCookieSurvivesRetryAndClearsOnTerminalOrSuccess(t *testing.T) {
	application, manager, store, _ := newProtectedTestServer(t, nil)
	defer manager.Close()
	defer store.Close()
	handler := application.Handler()
	var ceremonyCookie *http.Cookie
	var deadline time.Time
	for attempt := 1; attempt <= 5; attempt++ {
		begin := httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/auth/sign-in/begin", strings.NewReader("{}"))
		begin.Header.Set("Origin", "https://shepherdr.private")
		begin.Header.Set("Content-Type", "application/json")
		if ceremonyCookie != nil {
			begin.AddCookie(ceremonyCookie)
		}
		beginResponse := httptest.NewRecorder()
		handler.ServeHTTP(beginResponse, begin)
		if beginResponse.Code != http.StatusOK {
			t.Fatalf("begin attempt %d returned %d: %s", attempt, beginResponse.Code, beginResponse.Body.String())
		}
		cookies := beginResponse.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != signInCeremonyCookie {
			t.Fatalf("begin attempt %d cookies=%+v", attempt, cookies)
		}
		ceremonyCookie = cookies[0]
		if attempt == 1 {
			deadline = ceremonyCookie.Expires
		} else if !ceremonyCookie.Expires.Equal(deadline) {
			t.Fatalf("begin attempt %d extended cookie deadline to %v, want %v", attempt, ceremonyCookie.Expires, deadline)
		}

		finish := httptest.NewRequest(http.MethodPost, "https://shepherdr.private/api/auth/sign-in/finish", strings.NewReader("{}"))
		finish.Header.Set("Origin", "https://shepherdr.private")
		finish.Header.Set("Content-Type", "application/json")
		finish.AddCookie(ceremonyCookie)
		finishResponse := httptest.NewRecorder()
		handler.ServeHTTP(finishResponse, finish)
		if finishResponse.Code != http.StatusUnauthorized {
			t.Fatalf("finish attempt %d returned %d", attempt, finishResponse.Code)
		}
		finishCookies := finishResponse.Result().Cookies()
		if attempt < 5 && len(finishCookies) != 0 {
			t.Fatalf("ordinary failure %d changed ceremony cookie: %+v", attempt, finishCookies)
		}
		if attempt == 5 && (len(finishCookies) != 1 || finishCookies[0].Name != signInCeremonyCookie || finishCookies[0].MaxAge != -1) {
			t.Fatalf("terminal failure did not clear ceremony cookie: %+v", finishCookies)
		}
	}

	success := httptest.NewRecorder()
	finishCeremonyCookie(success, trustCeremonyCookie, nil)
	cookies := success.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != trustCeremonyCookie || cookies[0].MaxAge != -1 {
		t.Fatalf("successful ceremony did not clear its cookie: %+v", cookies)
	}
}

func serveProtected(handler http.Handler, method, path, origin, token string, jsonBody bool) *httptest.ResponseRecorder {
	var body *strings.Reader
	if jsonBody {
		body = strings.NewReader("{}")
	} else {
		body = strings.NewReader("")
	}
	request := httptest.NewRequest(method, "https://shepherdr.private"+path, body)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if jsonBody {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type protectedTestServerOptions struct {
	clock           access.Clock
	credentialCount int
	lifetime        string
	terminalLab     bool
}

type protectedTestServer struct {
	application *Server
	manager     *access.Manager
	store       *access.Store
	tokens      []string
	trustIDs    []string
}

type controlledAccessClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (c *controlledAccessClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *controlledAccessClock) Advance(duration time.Duration) {
	c.mutex.Lock()
	c.now = c.now.Add(duration)
	c.mutex.Unlock()
}

func newProtectedTestServer(t *testing.T, bridge *TerminalBridge) (*Server, *access.Manager, *access.Store, string) {
	t.Helper()
	server := newProtectedTestServerWithOptions(t, bridge, protectedTestServerOptions{credentialCount: 1})
	return server.application, server.manager, server.store, server.tokens[0]
}

func newProtectedTestServerWithOptions(t *testing.T, bridge *TerminalBridge, options protectedTestServerOptions) protectedTestServer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "access.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if options.credentialCount == 0 {
		options.credentialCount = 1
	}
	if options.lifetime == "" {
		options.lifetime = "30d"
	}
	now := time.Now().UTC()
	if options.clock != nil {
		now = options.clock.Now()
	}
	lifetime, err := access.ParseSessionLifetime(options.lifetime)
	if err != nil {
		t.Fatal(err)
	}
	state := access.State{
		Version: 1, PublicOrigin: "https://shepherdr.private", RPID: "shepherdr.private", SessionLifetime: options.lifetime,
		UserHandle:  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 64)),
		Credentials: []access.CredentialRecord{}, Sessions: []access.SessionRecord{},
		Invitations: []access.InvitationRecord{},
	}
	tokens := make([]string, 0, options.credentialCount)
	trustIDs := make([]string, 0, options.credentialCount)
	for index := 0; index < options.credentialCount; index++ {
		trustID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(3 + index)}, 16))
		token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(20 + index)}, 32))
		digest := sha256.Sum256([]byte(token))
		state.Credentials = append(state.Credentials, access.CredentialRecord{
			TrustID: trustID, Label: "Device <script>", CreatedAt: now, LastUsedAt: now, BackupObservedAt: now,
			Credential: webauthn.Credential{ID: []byte{byte(1 + index*2)}, PublicKey: []byte{byte(2 + index*2)}},
		})
		session := access.SessionRecord{
			Digest: base64.RawURLEncoding.EncodeToString(digest[:]), TrustID: trustID, CreatedAt: now, Lifetime: options.lifetime,
			FreshTrustID: trustID, FreshAt: now,
		}
		if !lifetime.None {
			expires := now.Add(lifetime.Duration())
			session.ExpiresAt = &expires
		}
		state.Sessions = append(state.Sessions, session)
		tokens = append(tokens, token)
		trustIDs = append(trustIDs, trustID)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := access.OpenProtected(access.OpenOptions{Path: path, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := access.NewManager(opened.Store, options.clock)
	if err != nil {
		opened.Store.Close()
		t.Fatal(err)
	}
	application := New(fstest.MapFS{"index.html": {Data: []byte("home")}}, nil, bridge, options.terminalLab)
	application.ConfigureProtectedAccess(manager)
	return protectedTestServer{application: application, manager: manager, store: opened.Store, tokens: tokens, trustIDs: trustIDs}
}

func dialProtectedWebSocket(t *testing.T, serverURL, path, token string) *websocket.Conn {
	t.Helper()
	local, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := websocket.Dialer{NetDialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, local.Host)
	}}
	header := http.Header{}
	header.Set("Origin", "https://shepherdr.private")
	header.Set("Cookie", sessionCookieName+"="+token)
	connection, response, err := dialer.Dial("ws://shepherdr.private"+path, header)
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("dial protected WebSocket: %v (status %d)", err, status)
	}
	return connection
}

func signOutTestSession(t *testing.T, manager *access.Manager, token string) {
	t.Helper()
	identity, ok := manager.AuthenticateToken(token)
	if !ok {
		t.Fatal("test session was not active")
	}
	runtimes, err := manager.SignOut(identity)
	if err != nil {
		t.Fatal(err)
	}
	access.WaitRuntimes(runtimes)
}
