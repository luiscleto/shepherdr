package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luisc/shepherdr/internal/herdr"
)

const homeHeartbeatInterval = 2 * time.Second

type Server struct {
	assets             fs.FS
	epoch              string
	projector          *herdr.Projector
	workspaceActions   *workspaceActionCoordinator
	terminal           *TerminalBridge
	terminalLabEnabled bool
	upgrader           websocket.Upgrader
}

func New(assets fs.FS, projector *herdr.Projector, terminal *TerminalBridge, terminalLabEnabled bool, actionClients ...*herdr.Client) *Server {
	server := &Server{
		assets:             assets,
		epoch:              newServerEpoch(),
		projector:          projector,
		terminal:           terminal,
		terminalLabEnabled: terminalLabEnabled,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 5 * time.Second,
		},
	}
	if len(actionClients) > 0 && actionClients[0] != nil {
		var refresh homeRefresher
		if projector != nil {
			refresh = projector
		}
		server.workspaceActions = newWorkspaceActionCoordinator(actionClients[0], refresh)
	}
	return server
}

func newServerEpoch() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("create server epoch: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/home", s.homeSocket)
	if s.workspaceActions != nil {
		mux.HandleFunc("POST /api/workspace-actions/prepare", s.workspaceActions.prepare)
		mux.HandleFunc("POST /api/workspace-actions", s.workspaceActions.run)
	}
	if s.terminal != nil {
		mux.HandleFunc("GET /api/terminal", s.terminalSocket)
		mux.HandleFunc("GET /api/terminal/read", s.terminalRead)
	}
	if s.terminalLabEnabled && s.terminal != nil {
		mux.HandleFunc("GET /api/terminal-lab", s.terminal.socket)
		mux.HandleFunc("GET /api/terminal-lab/read", s.terminal.read)
	}
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /", s.asset)
	return securityHeaders(mux, s.terminalLabEnabled)
}

func (s *Server) terminalSocket(writer http.ResponseWriter, request *http.Request) {
	s.terminal.productionSocket(writer, request)
}

func (s *Server) terminalRead(writer http.ResponseWriter, request *http.Request) {
	s.terminal.productionRead(writer, request)
}

func ValidateListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("Shepherdr must listen on localhost (127.0.0.1, [::1], or localhost)")
	}
	return nil
}

func (s *Server) homeSocket(writer http.ResponseWriter, request *http.Request) {
	connection, err := s.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(1024)

	updates, unsubscribe := s.projector.Subscribe()
	defer unsubscribe()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}()

	heartbeat := time.NewTicker(homeHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case state, ok := <-updates:
			if !ok {
				return
			}
			if err := writeJSON(connection, struct {
				herdr.State
				Epoch string `json:"epoch"`
			}{State: state, Epoch: s.epoch}); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := writeJSON(connection, map[string]string{"type": "home.heartbeat", "epoch": s.epoch}); err != nil {
				return
			}
		case <-closed:
			return
		case <-request.Context().Done():
			return
		}
	}
}

func (s *Server) asset(writer http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if strings.HasPrefix(name, "api/") {
		http.NotFound(writer, request)
		return
	}
	if name == "." || name == "" {
		name = "index.html"
	}
	if name == "terminal-lab" {
		name = "terminal-lab.html"
	}
	if isTerminalLabAsset(name) && !s.terminalLabEnabled {
		http.NotFound(writer, request)
		return
	}
	data, err := fs.ReadFile(s.assets, name)
	if err != nil && !strings.HasPrefix(name, "api/") && !isTerminalLabAsset(name) {
		name = "index.html"
		data, err = fs.ReadFile(s.assets, name)
	}
	if err != nil {
		http.Error(writer, "Browser assets are not built. Run the production build first.", http.StatusServiceUnavailable)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType != "" {
		writer.Header().Set("Content-Type", contentType)
	}
	if isTerminalLabAsset(name) {
		writer.Header().Set("Cache-Control", "no-store")
	} else {
		writer.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = writer.Write(data)
}

func isTerminalLabAsset(name string) bool {
	return name == "terminal-lab.html" || name == "terminal-lab.js" || name == "terminal-lab.css" || name == "ghostty-vt.wasm"
}

func securityHeaders(next http.Handler, terminalLabEnabled bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The terminal renderers apply validated color and geometry values through
		// element styles. Terminal content is always inserted as text, never HTML.
		styleSource := "style-src 'self' 'unsafe-inline'"
		scriptSource := "script-src 'self'"
		if terminalLabEnabled && request.URL.Path == "/terminal-lab" {
			// One development-only candidate uses a WebAssembly terminal core.
			scriptSource = "script-src 'self' 'wasm-unsafe-eval'"
		}
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; "+scriptSource+"; "+styleSource+"; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(writer, request)
	})
}

func writeJSON(connection *websocket.Conn, value any) error {
	_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return connection.WriteJSON(value)
}
