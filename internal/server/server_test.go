package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestApplicationAssetsAlwaysRevalidate(t *testing.T) {
	application := New(fstest.MapFS{
		"index.html": {Data: []byte("home")},
		"app.js":     {Data: []byte("application")},
	}, nil, nil, false)
	for _, target := range []string{"/", "/app.js"} {
		request := httptest.NewRequest("GET", "http://localhost"+target, nil)
		response := httptest.NewRecorder()
		application.Handler().ServeHTTP(response, request)
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
	first := New(nil, nil, nil, false)
	second := New(nil, nil, nil, false)
	if first.epoch == "" || second.epoch == "" {
		t.Fatal("server epoch must be present")
	}
	if first.epoch == second.epoch {
		t.Fatalf("independent server instances shared epoch %q", first.epoch)
	}
}

func TestSecurityHeadersConfineBrowserContent(t *testing.T) {
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	response := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(204)
	}), false).ServeHTTP(response, request)
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "object-src 'none'") || !strings.Contains(policy, "connect-src 'self'") || !strings.Contains(policy, "img-src 'self' data: blob:") {
		t.Fatalf("unexpected content security policy %q", policy)
	}
}

func TestProductionTerminalRequiresCurrentHerdrTarget(t *testing.T) {
	application := New(nil, nil, &TerminalBridge{}, false)
	request := httptest.NewRequest("GET", "http://localhost/api/terminal?pane=w1:p1&terminal=term-1&mode=observe&cols=80&rows=24", nil)
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("terminal without current Herdr truth returned %d, want 404", response.Code)
	}
}
