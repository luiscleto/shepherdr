package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/luiscleto/shepherdr/internal/notifications"
)

func TestNotificationRoutesRequireStrictSameOriginJSONAndExposeNoSecrets(t *testing.T) {
	store, err := notifications.OpenStore(filepath.Join(t.TempDir(), "notifications.json"), "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	manager := notifications.NewManager(store, nil)
	application := New(nil, nil, nil, false)
	application.SetNotifications(manager)
	handler := application.Handler()

	missingOrigin := notificationRequest(http.MethodPost, "/api/notifications/config", `{}`)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingOrigin)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("missing Origin returned %d, want 403", missingResponse.Code)
	}

	crossOrigin := notificationRequest(http.MethodPost, "/api/notifications/config", `{}`)
	crossOrigin.Header.Set("Origin", "https://elsewhere.example")
	crossResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossResponse, crossOrigin)
	if crossResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request returned %d, want 403", crossResponse.Code)
	}

	unknownField := notificationRequest(http.MethodPost, "/api/notifications/config", `{"extra":true}`)
	unknownField.Header.Set("Origin", "http://localhost")
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknownField)
	if unknownResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown field returned %d, want 400", unknownResponse.Code)
	}

	config := notificationRequest(http.MethodPost, "/api/notifications/config", `{}`)
	config.Header.Set("Origin", "http://localhost")
	configResponse := httptest.NewRecorder()
	handler.ServeHTTP(configResponse, config)
	if configResponse.Code != http.StatusOK {
		t.Fatalf("config returned %d: %s", configResponse.Code, configResponse.Body.String())
	}
	var response struct {
		Contact   string `json:"contact"`
		PublicKey string `json:"public_key"`
		Setup     bool   `json:"setup"`
	}
	if err := json.Unmarshal(configResponse.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Setup || response.Contact != "mailto:operator@example.com" || response.PublicKey == "" {
		t.Fatalf("config response = %+v", response)
	}
	for _, secretField := range []string{"vapid_private_key", "subscriptions", "endpoint", "p256dh", "auth"} {
		if strings.Contains(configResponse.Body.String(), secretField) {
			t.Fatalf("config response exposed %q: %s", secretField, configResponse.Body.String())
		}
	}
}

func TestNotificationSettingsReadDefaultsAndRemoveOnlyExactBrowser(t *testing.T) {
	store, err := notifications.OpenStore(filepath.Join(t.TempDir(), "notifications.json"), "https://operator.example/contact")
	if err != nil {
		t.Fatal(err)
	}
	manager := notifications.NewManager(store, nil)
	application := New(nil, nil, nil, false)
	application.SetNotifications(manager)
	handler := application.Handler()

	read := notificationRequest(http.MethodPost, "/api/notifications/settings/read", `{"endpoint":"https://push.example/browser-one"}`)
	read.Header.Set("Origin", "http://localhost")
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("settings read returned %d: %s", readResponse.Code, readResponse.Body.String())
	}
	var settings struct {
		Enabled bool                        `json:"enabled"`
		Events  notifications.EventSettings `json:"events"`
	}
	if err := json.Unmarshal(readResponse.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Enabled || settings.Events != notifications.DefaultEventSettings() {
		t.Fatalf("new browser settings = %+v", settings)
	}

	remove := notificationRequest(http.MethodDelete, "/api/notifications/settings", `{"endpoint":"https://push.example/browser-one"}`)
	remove.Header.Set("Origin", "http://localhost")
	removeResponse := httptest.NewRecorder()
	handler.ServeHTTP(removeResponse, remove)
	if removeResponse.Code != http.StatusOK || !strings.Contains(removeResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("remove returned %d: %s", removeResponse.Code, removeResponse.Body.String())
	}
}

func TestWebManifestUsesItsStandardsMediaType(t *testing.T) {
	application := New(fstest.MapFS{
		"manifest.webmanifest": {Data: []byte(`{"name":"Shepherdr"}`)},
	}, nil, nil, false)
	request := httptest.NewRequest(http.MethodGet, "http://localhost/manifest.webmanifest", nil)
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("manifest response status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func notificationRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, "http://localhost"+target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
