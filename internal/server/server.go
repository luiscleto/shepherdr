package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luisc/shepherdr/internal/herdr"
)

const homeHeartbeatInterval = 2 * time.Second

type Server struct {
	assets           fs.FS
	attachments      map[*terminalAttachment]struct{}
	controlMu        sync.Mutex
	controllers      map[string]*terminalChild
	epoch            string
	herdrBinary      string
	lifecycleChanged chan struct{}
	lifecycleMu      sync.Mutex
	logger           *slog.Logger
	projector        *herdr.Projector
	socketPath       string
	terminalCancel   context.CancelFunc
	terminalClose    sync.Once
	terminalCtx      context.Context
	upgrader         websocket.Upgrader
}

func New(assets fs.FS, projector *herdr.Projector, socketPath, herdrBinary string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	terminalCtx, terminalCancel := context.WithCancel(context.Background())
	return &Server{
		assets:           assets,
		attachments:      make(map[*terminalAttachment]struct{}),
		controllers:      make(map[string]*terminalChild),
		epoch:            newServerEpoch(),
		herdrBinary:      herdrBinary,
		lifecycleChanged: make(chan struct{}),
		logger:           logger,
		projector:        projector,
		socketPath:       socketPath,
		terminalCancel:   terminalCancel,
		terminalCtx:      terminalCtx,
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

func (s *Server) Close(ctx context.Context) error {
	s.terminalClose.Do(func() {
		if s.terminalCancel != nil {
			s.terminalCancel()
		}
	})
	for {
		s.lifecycleMu.Lock()
		if len(s.attachments) == 0 {
			s.lifecycleMu.Unlock()
			return nil
		}
		changed := s.lifecycleChanged
		s.lifecycleMu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/home", s.homeSocket)
	mux.HandleFunc("GET /api/terminal", s.terminalSocket)
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /", s.asset)
	return securityHeaders(mux)
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
	data, err := fs.ReadFile(s.assets, name)
	if err != nil && !strings.HasPrefix(name, "api/") {
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
	if name == "index.html" {
		writer.Header().Set("Cache-Control", "no-store")
	} else {
		writer.Header().Set("Cache-Control", "public, max-age=3600")
	}
	_, _ = writer.Write(data)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
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

func readClientMessage(connection *websocket.Conn, messages chan<- terminalClientMessage, closed chan<- struct{}) {
	defer close(closed)
	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			return
		}
		var message terminalClientMessage
		if len(data) > 64*1024 || json.Unmarshal(data, &message) != nil {
			continue
		}
		select {
		case messages <- message:
		default:
		}
	}
}

func (s *Server) logTerminalError(message string, err error) {
	if err != nil {
		s.logger.Info(message, "error", err)
	}
}
