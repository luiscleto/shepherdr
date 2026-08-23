package server

import (
	"bytes"
	"context"
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
	"github.com/luisc/shepherdr/internal/access"
	"github.com/luisc/shepherdr/internal/herdr"
	"github.com/luisc/shepherdr/internal/notifications"
	"github.com/luisc/shepherdr/internal/uploads"
)

const homeHeartbeatInterval = 2 * time.Second

type Server struct {
	access             *access.Manager
	assets             fs.FS
	epoch              string
	projector          *herdr.Projector
	workspaceActions   *workspaceActionCoordinator
	terminal           *TerminalBridge
	fileUploads        *uploads.Manager
	fileUploadLimit    uploads.Limit
	fileUploadState    TerminalStateSource
	notifications      *notifications.Manager
	origin             access.Origin
	signInOff          bool
	terminalLabEnabled bool
	upgrader           websocket.Upgrader
}

func (s *Server) SetNotifications(manager *notifications.Manager) {
	s.notifications = manager
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
	if s.access != nil {
		s.registerAccessRoutes(mux)
	}
	mux.HandleFunc("GET /api/home", s.homeSocket)
	if s.workspaceActions != nil {
		mux.HandleFunc("POST /api/workspace-actions/prepare", s.workspaceActions.prepare)
		mux.HandleFunc("POST /api/workspace-actions", s.workspaceActions.run)
	}
	if s.terminal != nil {
		mux.HandleFunc("GET /api/terminal", s.terminalSocket)
		mux.HandleFunc("GET /api/terminal/read", s.terminalRead)
		if s.fileUploads != nil && s.fileUploadState != nil {
			mux.HandleFunc("POST /api/terminal/files", s.terminalFileSend)
		}
	}
	if s.notifications != nil {
		mux.HandleFunc("POST /api/notifications/config", s.notificationConfig)
		mux.HandleFunc("POST /api/notifications/settings/read", s.notificationSettingsRead)
		mux.HandleFunc("POST /api/notifications/settings", s.notificationSettingsSave)
		mux.HandleFunc("DELETE /api/notifications/settings", s.notificationSettingsRemove)
	}
	if s.terminalLabEnabled && s.terminal != nil {
		mux.HandleFunc("GET /api/terminal-lab", s.terminalLabSocket)
		mux.HandleFunc("GET /api/terminal-lab/read", s.terminalLabRead)
	}
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /", s.asset)
	return securityHeaders(s.accessBoundary(mux), s.terminalLabEnabled)
}

func (s *Server) terminalSocket(writer http.ResponseWriter, request *http.Request) {
	s.boundTerminalSocket(writer, request, s.terminal.productionSocket)
}

func (s *Server) terminalLabSocket(writer http.ResponseWriter, request *http.Request) {
	s.boundTerminalSocket(writer, request, s.terminal.socket)
}

func (s *Server) boundTerminalSocket(writer http.ResponseWriter, request *http.Request, serve http.HandlerFunc) {
	request, lease, ok := s.bindAccessRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	if lease != nil {
		defer lease.Close()
		defer cleanupBoundRequest(request)
	}
	serve(writer, request)
}

func (s *Server) terminalRead(writer http.ResponseWriter, request *http.Request) {
	s.boundTerminalRead(writer, request, s.terminal.productionRead)
}

func (s *Server) terminalLabRead(writer http.ResponseWriter, request *http.Request) {
	s.boundTerminalRead(writer, request, s.terminal.read)
}

func (s *Server) boundTerminalRead(writer http.ResponseWriter, request *http.Request, serve http.HandlerFunc) {
	request, lease, ok := s.bindAccessRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	if lease == nil {
		serve(writer, request)
		return
	}
	defer lease.Close()
	defer cleanupBoundRequest(request)
	capture := newBufferedResponse()
	serve(capture, request)
	if err := withCommitAuthority(request, func() error {
		capture.Commit(writer)
		return nil
	}); err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
	}
}

func (s *Server) bindAccessRequest(request *http.Request) (*http.Request, *access.SessionLease, bool) {
	if s.access == nil {
		return request, nil, true
	}
	session, ok := sessionFromRequest(request)
	if !ok {
		return request, nil, false
	}
	lease, ok := s.access.Bind(session)
	if !ok {
		return request, nil, false
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(lease.Context(), cancel)
	ctx = context.WithValue(ctx, accessRequestCleanupKey{}, func() {
		stop()
		cancel()
	})
	return request.WithContext(ctx), lease, true
}

type accessRequestCleanupKey struct{}

func cleanupBoundRequest(request *http.Request) {
	if cleanup, ok := request.Context().Value(accessRequestCleanupKey{}).(func()); ok {
		cleanup()
	}
}

type bufferedResponse struct {
	body   bytes.Buffer
	header http.Header
	status int
}

func newBufferedResponse() *bufferedResponse    { return &bufferedResponse{header: make(http.Header)} }
func (r *bufferedResponse) Header() http.Header { return r.header }
func (r *bufferedResponse) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *bufferedResponse) Write(value []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(value)
}
func (r *bufferedResponse) Commit(writer http.ResponseWriter) {
	for name, values := range r.header {
		writer.Header()[name] = append([]string(nil), values...)
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(r.body.Bytes())
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
	var sessionLease *access.SessionLease
	if s.access != nil {
		session, ok := sessionFromRequest(request)
		if !ok {
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return
		}
		var bound bool
		sessionLease, bound = s.access.Bind(session)
		if !bound {
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return
		}
		defer sessionLease.Close()
	}
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
			if err := withCommitAuthority(request, func() error {
				return writeJSON(connection, struct {
					herdr.State
					Epoch string `json:"epoch"`
				}{State: state, Epoch: s.epoch})
			}); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := withCommitAuthority(request, func() error {
				return writeJSON(connection, map[string]string{"type": "home.heartbeat", "epoch": s.epoch})
			}); err != nil {
				return
			}
		case <-closed:
			return
		case <-request.Context().Done():
			return
		case <-sessionDone(sessionLease):
			return
		}
	}
}

func sessionDone(lease *access.SessionLease) <-chan struct{} {
	if lease == nil {
		return nil
	}
	return lease.Context().Done()
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
	if path.Ext(name) == ".webmanifest" {
		contentType = "application/manifest+json"
	}
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
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; "+scriptSource+"; "+styleSource+"; img-src 'self' data: blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
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
