package server

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// slowBodyResult sends headers promising a body of length bytes, with the
// loopback Origin a sign-in-off server expects, then only the first byte,
// and reports how long the server took to answer or close.
func slowBodyResult(t *testing.T, address, path string, length int) (time.Duration, string) {
	t.Helper()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	started := time.Now()
	if _, err := io.WriteString(connection, "POST "+path+" HTTP/1.1\r\nHost: "+address+"\r\nOrigin: http://"+address+"\r\nContent-Type: application/json\r\nContent-Length: "+strconv.Itoa(length)+"\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	status, _ := bufio.NewReader(connection).ReadString('\n')
	return time.Since(started), strings.TrimSpace(status)
}

func TestSlowRequestBodyIsCutOff(t *testing.T) {
	previous := requestBodyTime
	requestBodyTime = 300 * time.Millisecond
	defer func() { requestBodyTime = previous }()
	server := signInOffServer(t, &Server{workspaceActions: newWorkspaceActionCoordinator(&fakeWorkspaceActionClient{}, nil)})
	elapsed, status := slowBodyResult(t, server.Listener.Addr().String(), "/api/workspace-actions/prepare", 1000)
	if elapsed > 3*time.Second || !strings.Contains(status, "400") {
		t.Fatalf("slow body answered %q after %v, want 400 soon after the body time", status, elapsed)
	}
}

func TestBodylessRequestKeepsItsContextPastTheBodyTime(t *testing.T) {
	previous := requestBodyTime
	requestBodyTime = 100 * time.Millisecond
	defer func() { requestBodyTime = previous }()
	ended := make(chan error, 1)
	server := httptest.NewServer(limitBodyTime(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
			ended <- request.Context().Err()
		case <-time.After(400 * time.Millisecond):
			ended <- nil
		}
	})))
	defer server.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := <-ended; err != nil {
		t.Fatalf("a request without a body was ended by the body time: %v", err)
	}
	ended = make(chan error, 1)
	response, err = http.Post(server.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := <-ended; err != nil {
		t.Fatalf("a request whose body was sent was ended by the body time: %v", err)
	}
}

func TestSlowFileSendBodyIsCutOff(t *testing.T) {
	previous := uploadBodyGrace
	uploadBodyGrace = 300 * time.Millisecond
	defer func() { uploadBodyGrace = previous }()
	server := signInOffServer(t, fileSendApplication(t))
	elapsed, status := slowBodyResult(t, server.Listener.Addr().String(), "/api/terminal/files?herdr=tui", 1000)
	if elapsed > 3*time.Second || !strings.Contains(status, "400") {
		t.Fatalf("slow file send answered %q after %v, want 400 soon after the grace", status, elapsed)
	}
}
