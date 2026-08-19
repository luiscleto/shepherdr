package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luisc/shepherdr/internal/herdr"
)

func TestRealProductionHomeAndTerminal(t *testing.T) {
	base := os.Getenv("SHEPHERDR_REAL_URL")
	paneID := os.Getenv("SHEPHERDR_REAL_PANE")
	if base == "" || paneID == "" {
		t.Skip("set SHEPHERDR_REAL_URL and SHEPHERDR_REAL_PANE for the bounded production-path check")
	}
	homeURL := websocketTestURL(t, base, "/api/home")
	homeSocket, _, err := websocket.DefaultDialer.Dial(homeURL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer homeSocket.Close()
	_ = homeSocket.SetReadDeadline(time.Now().Add(8 * time.Second))

	var homeMessage struct {
		herdr.State
		Epoch string `json:"epoch"`
	}
	state := homeMessage.State
	for state.Connection != herdr.ConnectionLive {
		if err := homeSocket.ReadJSON(&homeMessage); err != nil {
			t.Fatal(err)
		}
		state = homeMessage.State
	}
	if homeMessage.Epoch == "" {
		t.Fatal("Home publication did not identify its server epoch")
	}
	var target *herdr.Terminal
	terminalCount := 0
	for _, workspace := range state.Home.Workspaces {
		for _, tab := range workspace.Tabs {
			for index := range tab.Terminals {
				terminalCount++
				if tab.Terminals[index].PaneID == paneID {
					candidate := tab.Terminals[index]
					target = &candidate
				}
			}
		}
	}
	if target == nil {
		t.Fatalf("pane %q is not a current terminal target", paneID)
	}
	t.Logf("Home is live: %d workspaces, %d terminals, %d working, %d blocked", len(state.Home.Workspaces), terminalCount, state.Home.WorkingCount, state.Home.BlockedCount)

	terminalURL := websocketTestURL(t, base, "/api/terminal")
	query := terminalURL.Query()
	query.Set("pane", target.PaneID)
	query.Set("terminal", target.TerminalID)
	query.Set("gap", strconv.FormatUint(state.Gap, 10))
	query.Set("cols", "80")
	query.Set("rows", "24")
	terminalURL.RawQuery = query.Encode()
	terminalSocket, _, err := websocket.DefaultDialer.Dial(terminalURL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer terminalSocket.Close()
	_ = terminalSocket.SetReadDeadline(time.Now().Add(10 * time.Second))

	gotFullFrame := false
	gotControl := false
	var fullFrameHash string
	for !gotControl {
		_, raw, err := terminalSocket.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var message terminalServerMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatal(err)
		}
		if message.Epoch != homeMessage.Epoch {
			t.Fatalf("terminal message epoch %q does not match Home epoch %q", message.Epoch, homeMessage.Epoch)
		}
		switch message.Type {
		case "terminal.frame":
			if message.Full {
				gotFullFrame = true
				hash := sha256.Sum256([]byte(message.Bytes))
				fullFrameHash = fmt.Sprintf("%x", hash[:8])
			}
		case "terminal.status":
			if message.Mode == "controlled_elsewhere" {
				t.Fatal("the test pane was unexpectedly controlled elsewhere")
			}
			gotControl = message.Mode == "controlled"
		}
	}
	if !gotFullFrame {
		t.Fatal("control was acquired before a fresh full observer frame")
	}
	if err := terminalSocket.WriteJSON(map[string]any{"type": "terminal.release"}); err != nil {
		t.Fatal(err)
	}
	t.Logf("pane %s received fresh full frame %s, acquired free control without takeover, and requested release", paneID, fullFrameHash)
}

func websocketTestURL(t *testing.T, base, endpoint string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(strings.TrimRight(base, "/") + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	}
	return parsed
}
