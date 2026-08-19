package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	first := New(nil, nil)
	second := New(nil, nil)
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
	})).ServeHTTP(response, request)
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "object-src 'none'") || !strings.Contains(policy, "connect-src 'self'") {
		t.Fatalf("unexpected content security policy %q", policy)
	}
}
