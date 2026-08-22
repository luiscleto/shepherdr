package server

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"strings"

	"github.com/luisc/shepherdr/internal/access"
)

type accessContextKey uint8

const (
	sessionContextKey accessContextKey = iota
	authorityCheckContextKey
	authorityCommitContextKey
)

type authorityCommit func(func() error) error

func (s *Server) ConfigureProtectedAccess(manager *access.Manager) {
	s.access = manager
	s.origin = manager.Origin()
	s.signInOff = false
}

func (s *Server) ConfigureSignInOff() {
	s.access = nil
	s.signInOff = true
}

func sessionFromRequest(request *http.Request) (access.SessionIdentity, bool) {
	session, ok := request.Context().Value(sessionContextKey).(access.SessionIdentity)
	return session, ok
}

func requestAuthorityValid(request *http.Request) bool {
	check, ok := request.Context().Value(authorityCheckContextKey).(func() bool)
	return !ok || check()
}

func withCommitAuthority(request *http.Request, operation func() error) error {
	commit, ok := request.Context().Value(authorityCommitContextKey).(authorityCommit)
	if !ok {
		return operation()
	}
	return commit(operation)
}

func (s *Server) accessBoundary(next http.Handler) http.Handler {
	if s.access == nil {
		return next
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !s.origin.ValidateHost(request.Host) {
			writeAccessError(writer, http.StatusMisdirectedRequest, "This address cannot use Shepherdr.")
			return
		}
		classification := classifyProtectedRoute(request.URL.Path, request.Method)
		if classification == routeUnknown {
			if request.URL.Path == "/api" || strings.HasPrefix(request.URL.Path, "/api/") {
				writeAccessError(writer, http.StatusNotFound, "Not found.")
				return
			}
			next.ServeHTTP(writer, request)
			return
		}
		if isMutationMethod(request.Method) {
			if !s.origin.ValidateOriginHeader(request.Header.Values("Origin")) || !exactJSONContentType(request) {
				writeAccessError(writer, http.StatusForbidden, "This request cannot use Shepherdr.")
				return
			}
		}
		if classification == routeWebSocket && isWebSocketUpgrade(request) &&
			!s.origin.ValidateOriginHeader(request.Header.Values("Origin")) {
			writeAccessError(writer, http.StatusForbidden, "This request cannot use Shepherdr.")
			return
		}
		if classification == routePublic {
			next.ServeHTTP(writer, request)
			return
		}
		token, err := request.Cookie(sessionCookieName)
		if err != nil {
			deleteSessionCookie(writer)
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return
		}
		session, ok := s.access.AuthenticateToken(token.Value)
		if !ok {
			deleteSessionCookie(writer)
			writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
			return
		}
		contextWithSession := context.WithValue(request.Context(), sessionContextKey, session)
		contextWithSession = context.WithValue(contextWithSession, authorityCheckContextKey, func() bool {
			return s.access.Recheck(session)
		})
		contextWithSession = context.WithValue(contextWithSession, authorityCommitContextKey, authorityCommit(func(operation func() error) error {
			return s.access.WithSessionRead(session, func(string) error { return operation() })
		}))
		next.ServeHTTP(writer, request.WithContext(contextWithSession))
	})
}

type protectedRoute uint8

const (
	routeUnknown protectedRoute = iota
	routePublic
	routeProtected
	routeWebSocket
)

func classifyProtectedRoute(path, method string) protectedRoute {
	if method == http.MethodGet || method == http.MethodHead {
		switch path {
		case "/healthz":
			return routePublic
		case "/api/home", "/api/terminal", "/api/terminal-lab":
			if method == http.MethodGet {
				return routeWebSocket
			}
			return routeUnknown
		case "/api/terminal/read", "/api/terminal-lab/read", "/api/devices":
			return routeProtected
		}
		if path != "/api" && !strings.HasPrefix(path, "/api/") {
			return routePublic
		}
		return routeUnknown
	}
	if isMutationMethod(method) {
		switch path {
		case "/api/auth/sign-in/begin", "/api/auth/sign-in/finish", "/api/auth/trust/begin", "/api/auth/trust/finish":
			if method == http.MethodPost {
				return routePublic
			}
		case "/api/auth/reauthenticate/begin", "/api/auth/reauthenticate/finish", "/api/auth/sign-out",
			"/api/devices/invitations", "/api/devices/revoke", "/api/workspace-actions/prepare", "/api/workspace-actions",
			"/api/notifications/config", "/api/notifications/settings/read", "/api/notifications/settings":
			if method == http.MethodPost || path == "/api/notifications/settings" && method == http.MethodDelete {
				return routeProtected
			}
		}
	}
	return routeUnknown
}

func isMutationMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func exactJSONContentType(request *http.Request) bool {
	values := request.Header.Values("Content-Type")
	if len(values) != 1 || values[0] != "application/json" {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(values[0])
	return err == nil && mediaType == "application/json" && len(parameters) == 0
}

func isWebSocketUpgrade(request *http.Request) bool {
	return headerContainsToken(request.Header.Values("Connection"), "upgrade") && headerContainsToken(request.Header.Values("Upgrade"), "websocket")
}

func headerContainsToken(values []string, token string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func writeAccessError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
}
