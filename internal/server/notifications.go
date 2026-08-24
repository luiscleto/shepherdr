package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/luiscleto/shepherdr/internal/notifications"
)

const maxNotificationRequest = 16 << 10

type notificationEndpointRequest struct {
	Endpoint string `json:"endpoint"`
}

type notificationSaveRequest struct {
	Events       notifications.EventSettings       `json:"events"`
	Subscription notifications.BrowserSubscription `json:"subscription"`
}

func (s *Server) notificationConfig(writer http.ResponseWriter, request *http.Request) {
	var body struct{}
	if !s.readNotificationJSON(writer, request, &body) {
		return
	}
	contact, publicKey, configured, err := s.notifications.Config()
	if err != nil {
		writeNotificationJSON(writer, http.StatusServiceUnavailable, struct {
			Setup       bool `json:"setup"`
			Unavailable bool `json:"unavailable"`
		}{Unavailable: true})
		return
	}
	writeNotificationJSON(writer, http.StatusOK, struct {
		Contact   string `json:"contact,omitempty"`
		PublicKey string `json:"public_key,omitempty"`
		Setup     bool   `json:"setup"`
	}{Contact: contact, PublicKey: publicKey, Setup: configured})
}

func (s *Server) notificationSettingsRead(writer http.ResponseWriter, request *http.Request) {
	var body notificationEndpointRequest
	if !s.readNotificationJSON(writer, request, &body) {
		return
	}
	var events notifications.EventSettings
	var enabled bool
	var err error
	if s.access != nil {
		session, ok := sessionFromRequest(request)
		if !ok {
			writeNotificationError(writer, http.StatusUnauthorized)
			return
		}
		err = s.access.WithSessionRead(session, func(trustID string) error {
			events, enabled, err = s.notifications.LookupOwned(body.Endpoint, trustID)
			return err
		})
	} else {
		events, enabled, err = s.notifications.Lookup(body.Endpoint)
	}
	if err != nil {
		writeNotificationError(writer, http.StatusBadRequest)
		return
	}
	writeNotificationJSON(writer, http.StatusOK, struct {
		Enabled bool                        `json:"enabled"`
		Events  notifications.EventSettings `json:"events"`
	}{Enabled: enabled, Events: events})
}

func (s *Server) notificationSettingsSave(writer http.ResponseWriter, request *http.Request) {
	var body notificationSaveRequest
	if !s.readNotificationJSON(writer, request, &body) {
		return
	}
	if s.access == nil {
		if err := s.notifications.Save(request.Context(), body.Subscription, body.Events); err != nil {
			writeNotificationError(writer, http.StatusBadRequest)
			return
		}
	} else {
		session, ok := sessionFromRequest(request)
		if !ok {
			writeNotificationError(writer, http.StatusUnauthorized)
			return
		}
		subscription, err := s.notifications.Prepare(request.Context(), body.Subscription, body.Events)
		if err == nil {
			err = s.access.WithSessionCommit(session, func(trustID string) error {
				return s.notifications.CommitOwned(subscription, trustID)
			})
		}
		if err != nil {
			writeNotificationError(writer, http.StatusBadRequest)
			return
		}
	}
	writeNotificationJSON(writer, http.StatusOK, struct {
		Enabled bool                        `json:"enabled"`
		Events  notifications.EventSettings `json:"events"`
	}{Enabled: true, Events: body.Events})
}

func (s *Server) notificationSettingsRemove(writer http.ResponseWriter, request *http.Request) {
	var body notificationEndpointRequest
	if !s.readNotificationJSON(writer, request, &body) {
		return
	}
	var err error
	if s.access == nil {
		_, err = s.notifications.Remove(body.Endpoint)
	} else {
		session, ok := sessionFromRequest(request)
		if !ok {
			writeNotificationError(writer, http.StatusUnauthorized)
			return
		}
		err = s.access.WithSessionCommit(session, func(trustID string) error {
			_, removeErr := s.notifications.RemoveOwned(body.Endpoint, trustID)
			return removeErr
		})
	}
	if err != nil {
		writeNotificationError(writer, http.StatusBadRequest)
		return
	}
	writeNotificationJSON(writer, http.StatusOK, struct {
		Enabled bool `json:"enabled"`
	}{Enabled: false})
}

func (s *Server) readNotificationJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	if s.access == nil && !strictSameOrigin(request) {
		writeNotificationError(writer, http.StatusForbidden)
		return false
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeNotificationError(writer, http.StatusBadRequest)
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxNotificationRequest)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeNotificationError(writer, http.StatusBadRequest)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeNotificationError(writer, http.StatusBadRequest)
		return false
	}
	return true
}

func strictSameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Host, request.Host) {
		return false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	} else if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return parsed.Scheme == scheme
}

func writeNotificationError(writer http.ResponseWriter, status int) {
	writeNotificationJSON(writer, status, struct {
		Error string `json:"error"`
	}{Error: "The notification request could not be completed."})
}

func writeNotificationJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
