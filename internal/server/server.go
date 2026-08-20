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
	assets      fs.FS
	epoch       string
	projector   *herdr.Projector
	terminalLab *TerminalLab
	upgrader    websocket.Upgrader
}

func New(assets fs.FS, projector *herdr.Projector, terminalLab *TerminalLab) *Server {
	return &Server{
		assets:      assets,
		epoch:       newServerEpoch(),
		projector:   projector,
		terminalLab: terminalLab,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 5 * time.Second,
		},
	}
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
	if s.terminalLab != nil {
		mux.HandleFunc("GET /api/terminal-lab", s.terminalLab.socket)
		mux.HandleFunc("GET /api/terminal-lab/read", s.terminalLab.read)
	}
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /", s.asset)
	return securityHeaders(mux, s.terminalLab != nil)
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
	if name == "." || name == "" {
		name = "index.html"
	}
	if name == "terminal-lab" {
		name = "terminal-lab.html"
	}
	if isTerminalLabAsset(name) && s.terminalLab == nil {
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
	if name == "index.html" || isTerminalLabAsset(name) {
		writer.Header().Set("Cache-Control", "no-store")
	} else {
		writer.Header().Set("Cache-Control", "public, max-age=3600")
	}
	_, _ = writer.Write(data)
}

func isTerminalLabAsset(name string) bool {
	return name == "terminal-lab.html" || name == "terminal-lab.js" || name == "terminal-lab.css" || name == "ghostty-vt.wasm"
}

func securityHeaders(next http.Handler, terminalLabEnabled bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		styleSource := "style-src 'self'"
		scriptSource := "script-src 'self'"
		if terminalLabEnabled && request.URL.Path == "/terminal-lab" {
			// Both candidate renderers use element styles as part of their public DOM renderer.
			// Their cores are WebAssembly. Keep both relaxations scoped to the
			// development-only lab document.
			styleSource = "style-src 'self' 'unsafe-inline'"
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
