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
func slowBodyResult(t *testing.T, address, path, contentType string, length int) (time.Duration, string) {
	t.Helper()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	started := time.Now()
	if _, err := io.WriteString(connection, "POST "+path+" HTTP/1.1\r\nHost: "+address+"\r\nOrigin: http://"+address+"\r\nContent-Type: "+contentType+"\r\nContent-Length: "+strconv.Itoa(length)+"\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	status, _ := bufio.NewReader(connection).ReadString('\n')
	return time.Since(started), strings.TrimSpace(status)
}

func TestSlowRequestBodyIsCutOff(t *testing.T) {
	application := &Server{workspaceActions: newWorkspaceActionCoordinator(&fakeWorkspaceActionClient{}, nil)}
	application.SetBodyLimits(300*time.Millisecond, 0)
	server := signInOffServer(t, application)
	elapsed, status := slowBodyResult(t, server.Listener.Addr().String(), "/api/workspace-actions/prepare", "application/json", 1000)
	if elapsed > 3*time.Second || !strings.Contains(status, "400") {
		t.Fatalf("slow body answered %q after %v, want 400 soon after the body time", status, elapsed)
	}
}

func TestBodylessRequestKeepsItsContextPastTheBodyTime(t *testing.T) {
	ended := make(chan error, 1)
	server := httptest.NewServer(limitBodyTime(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
			ended <- request.Context().Err()
		case <-time.After(400 * time.Millisecond):
			ended <- nil
		}
	}), 100*time.Millisecond))
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
	elapsed, status := slowBodyResult(t, server.Listener.Addr().String(), "/api/terminal/files?herdr=tui", "application/octet-stream", 1000)
	if elapsed > 3*time.Second || !strings.Contains(status, "400") {
		t.Fatalf("slow file send answered %q after %v, want 400 soon after the grace", status, elapsed)
	}
}

func TestSlowFileSendIsNotCutOffWhenThePaceIsOff(t *testing.T) {
	previous := uploadBodyGrace
	uploadBodyGrace = 100 * time.Millisecond
	defer func() { uploadBodyGrace = previous }()
	application := fileSendApplication(t)
	application.SetBodyLimits(100*time.Millisecond, 0)
	server := signInOffServer(t, application)
	address := server.Listener.Addr().String()
	body := terminalFileBody(t, "", "slow.txt", []byte("slow"), false)
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := io.WriteString(connection, "POST /api/terminal/files?herdr=tui HTTP/1.1\r\nHost: "+address+"\r\nOrigin: http://"+address+"\r\nContent-Type: application/octet-stream\r\nContent-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	for _, part := range [][]byte{body[:10], body[10 : len(body)-2], body[len(body)-2:]} {
		time.Sleep(300 * time.Millisecond)
		if _, err := connection.Write(part); err != nil {
			t.Fatalf("the server cut off a slow send with the pace off: %v", err)
		}
	}
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	status, _ := bufio.NewReader(connection).ReadString('\n')
	if !strings.Contains(status, "200") {
		t.Fatalf("slow send with the pace off answered %q", status)
	}
}

// After a file send's body has ended, checking for extra bytes must not set
// the read deadline again: net/http reads the connection in the background
// then, and a deadline there would cancel the request while it forwards.
func TestEndedFileSendBodyKeepsItsContextWhileForwarding(t *testing.T) {
	previous := uploadBodyGrace
	uploadBodyGrace = 300 * time.Millisecond
	defer func() { uploadBodyGrace = previous }()
	ended := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paceUploadBody(writer, request, DefaultUploadMinRate)
		if _, err := io.ReadAll(request.Body); err != nil {
			ended <- err
			return
		}
		if err := requireBodyEnd(request.Body); err != nil {
			ended <- err
			return
		}
		select {
		case <-request.Context().Done():
			ended <- request.Context().Err()
		case <-time.After(800 * time.Millisecond):
			ended <- nil
		}
	}))
	defer server.Close()
	response, err := http.Post(server.URL, "application/octet-stream", strings.NewReader(strings.Repeat("x", 100)))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := <-ended; err != nil {
		t.Fatalf("a send whose body ended was cancelled while forwarding: %v", err)
	}
}
