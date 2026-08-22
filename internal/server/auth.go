package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/luisc/shepherdr/internal/access"
	"rsc.io/qr"
)

const (
	sessionCookieName     = "__Host-shepherdr_session"
	signInCeremonyCookie  = "__Host-shepherdr_ceremony_signin"
	trustCeremonyCookie   = "__Host-shepherdr_ceremony_trust"
	reauthCeremonyCookie  = "__Host-shepherdr_ceremony_reauth"
	maxAccessRequestBytes = 128 << 10
)

func (s *Server) registerAccessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/sign-in/begin", s.signInBegin)
	mux.HandleFunc("POST /api/auth/sign-in/finish", s.signInFinish)
	mux.HandleFunc("POST /api/auth/trust/begin", s.trustBegin)
	mux.HandleFunc("POST /api/auth/trust/finish", s.trustFinish)
	mux.HandleFunc("POST /api/auth/reauthenticate/begin", s.reauthenticateBegin)
	mux.HandleFunc("POST /api/auth/reauthenticate/finish", s.reauthenticateFinish)
	mux.HandleFunc("POST /api/auth/sign-out", s.signOut)
	mux.HandleFunc("GET /api/devices", s.devices)
	mux.HandleFunc("POST /api/devices/invitations", s.deviceInvitation)
	mux.HandleFunc("POST /api/devices/revoke", s.deviceRevoke)
}

func (s *Server) signInBegin(writer http.ResponseWriter, request *http.Request) {
	if !readAccessJSON(writer, request, &struct{}{}) {
		return
	}
	begin, err := s.access.BeginSignIn(cookieValue(request, signInCeremonyCookie))
	if err != nil {
		writeAccessError(writer, http.StatusServiceUnavailable, "Sign in is not available. Try again.")
		return
	}
	setCeremonyCookie(writer, signInCeremonyCookie, begin.ClientToken, begin.ExpiresAt, begin.TTL)
	writeAccessJSON(writer, http.StatusOK, begin.Options)
}

func (s *Server) signInFinish(writer http.ResponseWriter, request *http.Request) {
	clientToken := cookieValue(request, signInCeremonyCookie)
	boundAccessBody(writer, request)
	var old *access.SessionIdentity
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		if identity, ok := s.access.AuthenticateToken(cookie.Value); ok {
			old = &identity
		}
	}
	issue, err := s.access.FinishSignIn(clientToken, request, old)
	finishCeremonyCookie(writer, signInCeremonyCookie, err)
	if err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	setSessionCookie(writer, issue)
	access.WaitRuntimes(issue.Runtimes)
	writeAccessJSON(writer, http.StatusOK, map[string]bool{"signed_in": true})
}

func (s *Server) trustBegin(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Label string `json:"label"`
		Token string `json:"token"`
	}
	if !readAccessJSON(writer, request, &body) {
		return
	}
	begin, err := s.access.BeginTrust(cookieValue(request, trustCeremonyCookie), body.Token, body.Label)
	if err != nil {
		writeInvitationError(writer)
		return
	}
	setCeremonyCookie(writer, trustCeremonyCookie, begin.ClientToken, begin.ExpiresAt, begin.TTL)
	writeAccessJSON(writer, http.StatusOK, begin.Options)
}

func (s *Server) trustFinish(writer http.ResponseWriter, request *http.Request) {
	clientToken := cookieValue(request, trustCeremonyCookie)
	boundAccessBody(writer, request)
	var old *access.SessionIdentity
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		if identity, ok := s.access.AuthenticateToken(cookie.Value); ok {
			old = &identity
		}
	}
	issue, err := s.access.FinishTrust(clientToken, request, old)
	finishCeremonyCookie(writer, trustCeremonyCookie, err)
	if err != nil {
		writeInvitationError(writer)
		return
	}
	setSessionCookie(writer, issue)
	access.WaitRuntimes(issue.Runtimes)
	writeAccessJSON(writer, http.StatusOK, map[string]bool{"trusted": true})
}

func (s *Server) reauthenticateBegin(writer http.ResponseWriter, request *http.Request) {
	if !readAccessJSON(writer, request, &struct{}{}) {
		return
	}
	session, ok := sessionFromRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	begin, err := s.access.BeginReauthentication(cookieValue(request, reauthCeremonyCookie), session)
	if err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	setCeremonyCookie(writer, reauthCeremonyCookie, begin.ClientToken, begin.ExpiresAt, begin.TTL)
	writeAccessJSON(writer, http.StatusOK, begin.Options)
}

func (s *Server) reauthenticateFinish(writer http.ResponseWriter, request *http.Request) {
	session, ok := sessionFromRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	clientToken := cookieValue(request, reauthCeremonyCookie)
	boundAccessBody(writer, request)
	issue, err := s.access.FinishReauthentication(clientToken, request, session)
	finishCeremonyCookie(writer, reauthCeremonyCookie, err)
	if err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	setSessionCookie(writer, issue)
	access.WaitRuntimes(issue.Runtimes)
	writeAccessJSON(writer, http.StatusOK, map[string]bool{"verified": true})
}

func (s *Server) signOut(writer http.ResponseWriter, request *http.Request) {
	if !readAccessJSON(writer, request, &struct{}{}) {
		return
	}
	session, ok := sessionFromRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	runtimes, err := s.access.SignOut(session)
	deleteSessionCookie(writer)
	if err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	access.WaitRuntimes(runtimes)
	writeAccessJSON(writer, http.StatusOK, map[string]bool{"signed_out": true})
}

func (s *Server) devices(writer http.ResponseWriter, request *http.Request) {
	session, ok := sessionFromRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	devices, err := s.access.Devices(session)
	if err != nil {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	writeAccessJSON(writer, http.StatusOK, struct {
		CanRevoke    bool            `json:"can_revoke"`
		CurrentTrust string          `json:"current_trust_id"`
		Devices      []access.Device `json:"devices"`
	}{CanRevoke: len(devices) > 1, CurrentTrust: session.TrustID, Devices: devices})
}

func (s *Server) deviceInvitation(writer http.ResponseWriter, request *http.Request) {
	if !readAccessJSON(writer, request, &struct{}{}) {
		return
	}
	session, ok := sessionFromRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	link, expiresAt, err := s.access.CreateBrowserInvitation(session)
	if errors.Is(err, access.ErrFreshRequired) {
		writeAccessError(writer, http.StatusPreconditionRequired, "Use a passkey before creating an invitation.")
		return
	}
	if err != nil {
		writeAccessError(writer, http.StatusConflict, "An invitation could not be created.")
		return
	}
	qrImage, err := invitationQRDataURL(link)
	if err != nil {
		writeAccessError(writer, http.StatusConflict, "An invitation could not be created.")
		return
	}
	writeAccessJSON(writer, http.StatusOK, struct {
		ExpiresAt time.Time `json:"expires_at"`
		Link      string    `json:"link"`
		QR        string    `json:"qr"`
	}{ExpiresAt: expiresAt, Link: link, QR: qrImage})
}

func invitationQRDataURL(link string) (string, error) {
	code, err := qr.Encode(link, qr.L)
	if err != nil {
		return "", err
	}
	code.Scale = 8
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}

func (s *Server) deviceRevoke(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		TrustID string `json:"trust_id"`
	}
	if !readAccessJSON(writer, request, &body) {
		return
	}
	session, ok := sessionFromRequest(request)
	if !ok {
		writeAccessError(writer, http.StatusUnauthorized, "Sign in again.")
		return
	}
	result, err := s.access.RevokeBrowser(session, body.TrustID)
	if errors.Is(err, access.ErrFreshRequired) {
		writeAccessError(writer, http.StatusPreconditionRequired, "Use a passkey before revoking this trusted sign-in.")
		return
	}
	if errors.Is(err, access.ErrLastCredential) {
		writeAccessError(writer, http.StatusConflict, "The final trusted sign-in cannot be revoked. Stop Shepherdr and reset access on the machine.")
		return
	}
	if err != nil {
		writeAccessError(writer, http.StatusConflict, "This trusted sign-in could not be revoked.")
		return
	}
	if body.TrustID == session.TrustID {
		deleteSessionCookie(writer)
	}
	access.WaitRuntimes(result.Runtimes)
	writeAccessJSON(writer, http.StatusOK, map[string]bool{"revoked": true})
}

func readAccessJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxAccessRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAccessError(writer, http.StatusBadRequest, "This request could not be completed.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAccessError(writer, http.StatusBadRequest, "This request could not be completed.")
		return false
	}
	return true
}

func boundAccessBody(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxAccessRequestBytes)
}

func cookieValue(request *http.Request, name string) string {
	cookie, err := request.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func ceremonyCookieName(kind string) string {
	switch kind {
	case access.CeremonyTrust:
		return trustCeremonyCookie
	case access.CeremonyReauth:
		return reauthCeremonyCookie
	default:
		return signInCeremonyCookie
	}
}

func setCeremonyCookie(writer http.ResponseWriter, name, value string, expiresAt time.Time, ttl time.Duration) {
	maxAge := max(1, int(ttl.Seconds()))
	http.SetCookie(writer, &http.Cookie{
		Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Expires: expiresAt.UTC(), MaxAge: maxAge,
	})
}

func deleteCeremonyCookie(writer http.ResponseWriter, name string) {
	http.SetCookie(writer, &http.Cookie{
		Name: name, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
	})
}

func finishCeremonyCookie(writer http.ResponseWriter, name string, err error) {
	if err == nil || !access.CeremonyCanRetry(err) {
		deleteCeremonyCookie(writer, name)
	}
}

func setSessionCookie(writer http.ResponseWriter, issue access.SessionIssue) {
	cookie := &http.Cookie{
		Name: sessionCookieName, Value: issue.Token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	}
	if issue.ExpiresAt != nil {
		cookie.Expires = issue.ExpiresAt.UTC()
		cookie.MaxAge = int(issue.ExpiresAt.Sub(issue.CreatedAt).Seconds())
	}
	http.SetCookie(writer, cookie)
}

func deleteSessionCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
	})
}

func writeInvitationError(writer http.ResponseWriter) {
	writeAccessError(writer, http.StatusBadRequest, "This invitation can't be used. Create a new invitation on the machine running Shepherdr or from a trusted device.")
}

func writeAccessJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
