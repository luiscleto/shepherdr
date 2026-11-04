package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/shares"
	"github.com/luiscleto/shepherdr/internal/uploads"
)

func sharedTestStore(t *testing.T, files map[string]string) (*shares.Store, map[string]string) {
	t.Helper()
	store, err := shares.Open(filepath.Join(t.TempDir(), "shared"), uploads.Limit{Unlimited: true}, uploads.Limit{Unlimited: true},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ids := map[string]string{}
	for name, content := range files {
		incoming, err := store.Begin(name, int64(len(content)))
		if err != nil {
			t.Fatal(err)
		}
		if err := incoming.Copy(context.Background(), strings.NewReader(content)); err != nil {
			t.Fatal(err)
		}
		record, err := incoming.Commit(nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = record.ID
	}
	return store, ids
}

func TestSharedRoutesAreInTheProtectedInventory(t *testing.T) {
	id := strings.Repeat("0f", 16)
	for _, test := range []struct {
		method, path string
		want         protectedRoute
	}{
		{http.MethodGet, "/api/shared/" + id, routeProtected},
		{http.MethodHead, "/api/shared/" + id, routeProtected},
		{http.MethodPost, "/api/shared/delete", routeProtected},
		{http.MethodGet, "/api/shared/" + strings.ToUpper(id), routeUnknown},
		{http.MethodGet, "/api/shared/../home", routeUnknown},
		{http.MethodGet, "/api/shared/delete", routeUnknown},
		{http.MethodPost, "/api/shared/" + id, routeUnknown},
		{http.MethodDelete, "/api/shared/delete", routeUnknown},
	} {
		if got := classifyProtectedRoute(test.path, test.method); got != test.want {
			t.Errorf("%s %s = %d, want %d", test.method, test.path, got, test.want)
		}
	}
}

func TestSharedFilesAreServedWithFixedConfiningHeaders(t *testing.T) {
	store, ids := sharedTestStore(t, map[string]string{
		"evil.html":        "<script>fetch('/api/home')</script>",
		"evil.svg":         `<svg xmlns="http://www.w3.org/2000/svg"><script>fetch('/api/home')</script></svg>`,
		"evil.png":         "<html><script>fetch('/api/home')</script></html>",
		"notes.md":         "# notes",
		"clip.mp4":         "0123456789",
		"naïve report.pdf": "%PDF-1.4",
	})
	application := New(nil, nil, herdr.NewNotFollowedProjector(), nil, false)
	application.SetShares(store)
	server := signInOffServer(t, application)
	get := func(method, name, query string, header http.Header) *http.Response {
		t.Helper()
		request, _ := http.NewRequest(method, server.URL+"/api/shared/"+ids[name]+query, nil)
		for key, values := range header {
			request.Header[key] = values
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	for _, test := range []struct {
		name, query, contentType, disposition string
	}{
		{"evil.html", "", "application/octet-stream", `attachment; filename=evil.html`},
		{"evil.svg", "", "image/svg+xml", `inline; filename=evil.svg`},
		{"evil.png", "", "image/png", `inline; filename=evil.png`},
		{"notes.md", "", "text/plain; charset=utf-8", `inline; filename=notes.md`},
		{"notes.md", "?download=1", "text/plain; charset=utf-8", `attachment; filename=notes.md`},
		{"naïve report.pdf", "", "application/pdf", `inline; filename*=utf-8''na%C3%AFve%20report.pdf`},
	} {
		response := get(http.MethodGet, test.name, test.query, nil)
		header := response.Header
		got := []string{header.Get("Content-Type"), header.Get("Content-Disposition"), header.Get("Content-Security-Policy"),
			header.Get("X-Content-Type-Options"), header.Get("Cross-Origin-Resource-Policy"), header.Get("Cache-Control"),
			header.Get("X-Frame-Options"), header.Get("Referrer-Policy")}
		want := []string{test.contentType, test.disposition, sharedFilePolicy, "nosniff", "same-origin", "private, no-store", "DENY", "no-referrer"}
		for index := range want {
			if got[index] != want[index] {
				t.Errorf("%s%s header %d = %q, want %q", test.name, test.query, index, got[index], want[index])
			}
		}
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s status %d", test.name, response.StatusCode)
		}
	}
	// Video needs ranges; HEAD sends no body.
	ranged := get(http.MethodGet, "clip.mp4", "", http.Header{"Range": {"bytes=2-4"}})
	body, _ := io.ReadAll(ranged.Body)
	if ranged.StatusCode != http.StatusPartialContent || string(body) != "234" {
		t.Fatalf("range = %d %q", ranged.StatusCode, body)
	}
	head := get(http.MethodHead, "clip.mp4", "", nil)
	if head.StatusCode != http.StatusOK || head.Header.Get("Content-Length") != "10" || head.Header.Get("Content-Security-Policy") != sharedFilePolicy {
		t.Fatalf("head = %d %v", head.StatusCode, head.Header)
	}
	// Errors carry the same fixed headers: a range past the end, an unknown
	// id and a malformed one.
	past := get(http.MethodGet, "clip.mp4", "", http.Header{"Range": {"bytes=100-"}})
	requireFixedSharedHeaders(t, "416", past, http.StatusRequestedRangeNotSatisfiable)
	for _, path := range []string{strings.Repeat("ab", 16), "xyz"} {
		missing, err := http.Get(server.URL + "/api/shared/" + path)
		if err != nil {
			t.Fatal(err)
		}
		missing.Body.Close()
		requireFixedSharedHeaders(t, "404 "+path, missing, http.StatusNotFound)
	}
}

func TestSharedFileRefusalsCarryTheFixedHeaders(t *testing.T) {
	application, manager, store, _ := newProtectedTestServer(t, nil)
	defer manager.Close()
	defer store.Close()
	shared, ids := sharedTestStore(t, map[string]string{"a.png": "png"})
	application.SetShares(shared)
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/shared/"+ids["a.png"], nil)
	request.Host = "shepherdr.private"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	requireFixedSharedHeaders(t, "no session", response, http.StatusUnauthorized)
}

func requireFixedSharedHeaders(t *testing.T, name string, response *http.Response, status int) {
	t.Helper()
	header := response.Header
	got := []string{header.Get("Content-Security-Policy"), header.Get("X-Content-Type-Options"), header.Get("Cross-Origin-Resource-Policy"),
		header.Get("Cache-Control"), header.Get("X-Frame-Options"), header.Get("Referrer-Policy")}
	want := []string{sharedFilePolicy, "nosniff", "same-origin", "private, no-store", "DENY", "no-referrer"}
	if response.StatusCode != status || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s: %d %q, want %d %q", name, response.StatusCode, got, status, want)
	}
}

func TestSharedDeleteRemovesExactlyThoseFilesAndUpdatesHome(t *testing.T) {
	store, ids := sharedTestStore(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	application := New(nil, herdr.NewProjector(nil), herdr.NewNotFollowedProjector(), nil, false)
	application.SetShares(store)
	server := signInOffServer(t, application)
	connection := signInOffDial(t, server.URL, "/api/home")
	defer connection.Close()
	readShared := func() []sharedFile {
		t.Helper()
		for {
			_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, data, err := connection.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			var message sharedMessage
			if json.Unmarshal(data, &message) == nil && message.Type == "shared" {
				return message.Files
			}
		}
	}
	first := readShared()
	if len(first) != 2 || first[0].Seen || first[0].Kind != shares.KindText || first[0].Tag != "TXT" || first[0].From != nil {
		t.Fatalf("first list = %+v", first)
	}
	// Showing the whole list marks every file seen, on every device.
	if err := connection.WriteMessage(1, []byte(`{"type":"shared.view","all":true}`)); err != nil {
		t.Fatal(err)
	}
	seen := readShared()
	if !seen[0].Seen || !seen[1].Seen {
		t.Fatalf("after view = %+v", seen)
	}
	post := func(contentType, body string) *http.Response {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/shared/delete", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", server.URL)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	if response := post("text/plain", `{"ids":["`+ids["a.txt"]+`"]}`); response.StatusCode == http.StatusOK {
		t.Fatal("a text/plain delete was accepted")
	}
	gone := strings.Repeat("cd", 16)
	response := post("application/json", `{"ids":["`+ids["a.txt"]+`","`+gone+`"]}`)
	var result sharedDeleteResponse
	json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(result.Deleted) != 1 || result.Deleted[0] != ids["a.txt"] || len(result.Gone) != 1 || result.Gone[0] != gone {
		t.Fatalf("delete = %d %+v", response.StatusCode, result)
	}
	after := readShared()
	if len(after) != 1 || after[0].ID != ids["b.txt"] {
		t.Fatalf("after delete = %+v", after)
	}
	deleted, _ := http.Get(server.URL + "/api/shared/" + ids["a.txt"])
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted id = %d", deleted.StatusCode)
	}
}

func TestPageAllowsSameOriginMediaOnly(t *testing.T) {
	application := New(nil, nil, herdr.NewNotFollowedProjector(), nil, false)
	server := signInOffServer(t, application)
	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if policy := response.Header.Get("Content-Security-Policy"); !strings.Contains(policy, "; media-src 'self';") {
		t.Fatalf("policy = %q", policy)
	}
}
