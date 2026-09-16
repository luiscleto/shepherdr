package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestTerminalKeyValidation(t *testing.T) {
	for _, base := range []string{"enter", "esc", "tab", "backspace", "left", "up", "down", "right", "f1", "F12", "é", "界", "😀", "A", " ", "+", "minus"} {
		for mask := 0; mask < 32; mask++ {
			key := terminalKey{Base: base, Ctrl: mask&1 != 0, Alt: mask&2 != 0, Shift: mask&4 != 0, Super: mask&8 != 0, Hyper: mask&16 != 0}
			canonical, err := key.canonical()
			if err != nil || canonical == "" {
				t.Fatalf("%+v: %q %v", key, canonical, err)
			}
		}
	}
	for _, base := range []string{"", "f0", "f13", "f255", "home", "delete", "ctrl+a", "a+b", "éa", "e\u0301", "\n", "\x00", "\u200b", "\u00a0"} {
		if value, err := (terminalKey{Base: base}).canonical(); err == nil {
			t.Fatalf("accepted %q as %q", base, value)
		}
	}
	for _, test := range []struct {
		key  terminalKey
		want string
	}{
		{terminalKey{Base: " ", Ctrl: true}, "ctrl+space"}, {terminalKey{Base: "+", Alt: true}, "alt+plus"}, {terminalKey{Base: "A"}, "A"},
	} {
		if got, _ := test.key.canonical(); got != test.want {
			t.Fatalf("%q != %q", got, test.want)
		}
	}
	for _, body := range []string{
		`{"type":"terminal.send-key","request_id":1,"key":{"base":"\ud800"}}`,
		`{"type":"terminal.send-key","request_id":1,"key":{"base":"a","ctrl":null}}`,
		`{"type":"terminal.send-key","request_id":1,"key":{"base":"f13"}}`,
		`{"type":"terminal.send-key","request_id":1,"key":{"base":"a","ctrl":"true"}}`,
		`{"type":"terminal.send-key","request_id":1,"key":{"base":"a","meta":true}}`,
		`{"type":"terminal.send-key","request_id":1,"key":{"base":"a"},"pane":"other"}`,
	} {
		if _, err := validatedTerminalCommand([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestTerminalKeyGuardsAndOutcomes(t *testing.T) {
	for _, scenario := range []string{"accepted", "observer", "pending", "released", "ended", "cancelled", "stale", "authority", "takeover", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			snapshot := terminalActionSnapshot()
			snapshot.Version, snapshot.Protocol = "0.9.0", 22
			snapshot.Layouts = []herdr.LayoutInfo{{TabID: "tab-one", WorkspaceID: "workspace", Panes: []herdr.LayoutPane{{PaneID: "pane-one", Rect: herdr.Rectangle{Width: 80, Height: 24}}}}}
			source := newFakeTerminalStateSource(herdr.State{Connection: herdr.ConnectionLive, Snapshot: snapshot})
			snapshot.Panes = append([]herdr.PaneInfo(nil), snapshot.Panes...)
			bridge := testTerminalBridge(source)
			defer bridge.Close()
			lease, ok := bridge.acquireTarget("pane-one", "terminal-one")
			if !ok {
				t.Fatal("lease")
			}
			defer lease.Close()
			session := &terminalChildSession{controlled: true, exited: make(chan struct{}), settings: terminalBridgeSettings{mode: "control"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest("GET", "/api/terminal", nil).WithContext(ctx)
			switch scenario {
			case "observer":
				session.settings.mode = "observe"
			case "pending", "released":
				session.controlled = false
			case "ended":
				close(session.exited)
			case "cancelled":
				cancel()
			case "stale":
				snapshot.Panes[0].TerminalID = "replacement"
			case "authority":
				request = request.WithContext(context.WithValue(ctx, authorityCommitContextKey, authorityCommit(func(func() error) error { return errors.New("signed out") })))
			}
			path := shortSocketPath(t)
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			bridge.keys = herdr.NewClient(path, bridge.logger)
			var sends atomic.Int32
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					var call struct {
						ID, Method string
						Params     struct {
							PaneID string   `json:"pane_id"`
							Keys   []string `json:"keys"`
						}
					}
					if json.NewDecoder(conn).Decode(&call) != nil {
						conn.Close()
						return
					}
					if call.Method == "session.snapshot" {
						if scenario == "takeover" {
							session.inputEnded.Store(true)
						}
						_ = json.NewEncoder(conn).Encode(map[string]any{"id": call.ID, "result": map[string]any{"type": "session_snapshot", "snapshot": snapshot}})
					} else {
						sends.Add(1)
						if call.Method != "pane.send_keys" || call.Params.PaneID != "pane-one" || strings.Join(call.Params.Keys, ",") != "ctrl+space" {
							t.Error("unexpected key RPC")
						}
						if scenario != "unknown" {
							_ = json.NewEncoder(conn).Encode(map[string]any{"id": call.ID, "result": map[string]string{"type": "ok"}})
						}
					}
					conn.Close()
				}
			}()
			session.writeMutex.Lock()
			outcome, err := bridge.sendTerminalKey(request, session, lease, "ctrl+space")
			session.writeMutex.Unlock()
			listener.Close()
			<-done
			want := terminalBatchNotSent
			wantSends := int32(0)
			if scenario == "accepted" {
				want, wantSends = terminalBatchForwarded, 1
			}
			if scenario == "unknown" {
				want, wantSends = terminalBatchUnknown, 1
			}
			if outcome != want || sends.Load() != wantSends {
				t.Fatalf("outcome=%v sends=%d err=%v", outcome, sends.Load(), err)
			}
		})
	}
}
