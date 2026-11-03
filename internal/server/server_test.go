package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestApplicationAssetsAlwaysRevalidate(t *testing.T) {
	application := New(fstest.MapFS{
		"index.html": {Data: []byte("home")},
		"app.js":     {Data: []byte("application")},
	}, nil, herdr.NewNotFollowedProjector(), nil, false)
	for _, target := range []string{"/", "/app.js"} {
		request := httptest.NewRequest("GET", "http://localhost"+target, nil)
		response := httptest.NewRecorder()
		signInOffHandler(application).ServeHTTP(response, request)
		if got := response.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache", target, got)
		}
	}
}

func TestValidateListenAddressAllowsOnlyLocalhost(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:0", "localhost:9000"} {
		if err := ValidateListenAddress(address); err != nil {
			t.Errorf("ValidateListenAddress(%q) = %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8787", "192.168.1.5:8787", ":8787"} {
		if err := ValidateListenAddress(address); err == nil {
			t.Errorf("ValidateListenAddress(%q) unexpectedly succeeded", address)
		}
	}
}

func TestServerEpochIsPresentAndProcessLocal(t *testing.T) {
	first := New(nil, nil, herdr.NewNotFollowedProjector(), nil, false)
	second := New(nil, nil, herdr.NewNotFollowedProjector(), nil, false)
	if first.epoch == "" || second.epoch == "" {
		t.Fatal("server epoch must be present")
	}
	if first.epoch == second.epoch {
		t.Fatalf("independent server instances shared epoch %q", first.epoch)
	}
}

// One Home message carries both Herdrs' complete states. herdr-core that is
// not followed is a fixed "not running" state without a Home.
func TestHomeMessageCarriesBothHerdrs(t *testing.T) {
	data := firstHomeMessage(t, New(nil, herdr.NewProjector(nil), herdr.NewNotFollowedProjector(), nil, false))
	var message map[string]json.RawMessage
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(message))
	for key := range message {
		keys = append(keys, key)
	}
	if len(keys) != 3 || message["epoch"] == nil || message["tui"] == nil || message["core"] == nil {
		t.Fatalf("Home message keys = %v, want epoch, tui and core: %s", keys, data)
	}
	var sides struct {
		TUI  map[string]any `json:"tui"`
		Core map[string]any `json:"core"`
	}
	if err := json.Unmarshal(data, &sides); err != nil {
		t.Fatal(err)
	}
	wantCore := map[string]any{
		"connection": "not_running", "gap": 0.0, "has_home": false, "herdr_version": "", "last_known": false,
		"home":               map[string]any{"workspaces": []any{}, "working_count": 0.0, "blocked_count": 0.0, "error_count": 0.0},
		"can_open_terminals": false,
	}
	if !reflect.DeepEqual(sides.Core, wantCore) {
		t.Fatalf("core side = %v, want %v", sides.Core, wantCore)
	}
	if sides.TUI["connection"] != "reconnecting" || sides.TUI["has_home"] != false {
		t.Fatalf("Herdr TUI side = %v", sides.TUI)
	}
	if _, ok := sides.TUI["herdr"]; ok {
		t.Fatalf("a side still names its Herdr: %v", sides.TUI)
	}
}

func TestHomeMessageSaysWhichTerminalsCanOpen(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal *TerminalBridge
		wantTUI  bool
	}{
		{name: "herdr not on PATH", terminal: nil, wantTUI: false},
		{name: "Herdr TUI terminal bridge", terminal: &TerminalBridge{}, wantTUI: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := firstHomeMessage(t, New(nil, herdr.NewProjector(nil), herdr.NewNotFollowedProjector(), test.terminal, false))
			var sides struct {
				TUI  map[string]any `json:"tui"`
				Core map[string]any `json:"core"`
			}
			if err := json.Unmarshal(data, &sides); err != nil {
				t.Fatal(err)
			}
			if sides.TUI["can_open_terminals"] != test.wantTUI || sides.Core["can_open_terminals"] != false {
				t.Fatalf("can_open_terminals: tui = %v, core = %v; want %t and false", sides.TUI["can_open_terminals"], sides.Core["can_open_terminals"], test.wantTUI)
			}
		})
	}
}

func firstHomeMessage(t *testing.T, application *Server) []byte {
	t.Helper()
	httpServer := signInOffServer(t, application)
	connection := signInOffDial(t, httpServer.URL, "/api/home")
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSecurityHeadersConfineBrowserContent(t *testing.T) {
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	response := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(204)
	}), false, false).ServeHTTP(response, request)
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "object-src 'none'") || !strings.Contains(policy, "connect-src 'self'") || !strings.Contains(policy, "img-src 'self' data: blob:") {
		t.Fatalf("unexpected content security policy %q", policy)
	}
	if got := response.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Fatalf("Cross-Origin-Opener-Policy = %q", got)
	}
	permissions := response.Header().Get("Permissions-Policy")
	for _, feature := range []string{"publickey-credentials", "clipboard", "camera", "microphone"} {
		if strings.Contains(permissions, feature) {
			t.Fatalf("Permissions-Policy %q restricts %s", permissions, feature)
		}
	}
	if !strings.Contains(permissions, "geolocation=()") || !strings.Contains(permissions, "usb=()") {
		t.Fatalf("Permissions-Policy %q", permissions)
	}
	if got := response.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("sign-in-off response sent Strict-Transport-Security %q", got)
	}
	protected := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(204)
	}), false, true).ServeHTTP(protected, request)
	if got := protected.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("protected Strict-Transport-Security = %q", got)
	}
}

func TestProductionTerminalRequiresCurrentHerdrTarget(t *testing.T) {
	application := New(nil, nil, herdr.NewNotFollowedProjector(), &TerminalBridge{}, false)
	request := httptest.NewRequest("GET", "http://localhost/api/terminal?herdr=tui&pane=w1:p1&terminal=term-1&mode=observe&cols=80&rows=24", nil)
	response := httptest.NewRecorder()
	signInOffHandler(application).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("terminal without current Herdr truth returned %d, want 404", response.Code)
	}
}
