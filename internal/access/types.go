package access

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	stateVersion         = 1
	maxStateBytes        = 1 << 20
	MaxCredentials       = 32
	MaxInvitations       = 32
	MaxAuthorizerInvites = 8
	MaxSessions          = 256
	MaxTrustSessions     = 32
	InvitationLifetime   = 10 * time.Minute
	ReservationLifetime  = 5 * time.Minute
	FreshLifetime        = 5 * time.Minute
)

type State struct {
	Version         int                `json:"version"`
	PublicOrigin    string             `json:"public_origin"`
	RPID            string             `json:"rp_id"`
	SessionLifetime string             `json:"session_lifetime"`
	UserHandle      string             `json:"user_handle"`
	Credentials     []CredentialRecord `json:"credentials"`
	Sessions        []SessionRecord    `json:"sessions"`
	Invitations     []InvitationRecord `json:"invitations"`
}

type CredentialRecord struct {
	TrustID          string              `json:"trust_id"`
	Label            string              `json:"label"`
	CreatedAt        time.Time           `json:"created_at"`
	LastUsedAt       time.Time           `json:"last_used_at"`
	BackupObservedAt time.Time           `json:"backup_observed_at"`
	RevokedAt        *time.Time          `json:"revoked_at,omitempty"`
	Credential       webauthn.Credential `json:"credential"`
}

type SessionRecord struct {
	Digest       string     `json:"digest"`
	TrustID      string     `json:"trust_id"`
	CreatedAt    time.Time  `json:"created_at"`
	Lifetime     string     `json:"lifetime"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	FreshTrustID string     `json:"fresh_trust_id"`
	FreshAt      time.Time  `json:"fresh_at"`
}

type InvitationRecord struct {
	Digest               string     `json:"digest"`
	IssuerTrustID        string     `json:"issuer_trust_id,omitempty"`
	IssuedAt             time.Time  `json:"issued_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
	ReservedClientDigest string     `json:"reserved_client_digest,omitempty"`
	ReservedUntil        *time.Time `json:"reserved_until,omitempty"`
	ConsumedAt           *time.Time `json:"consumed_at,omitempty"`
}

type Device struct {
	TrustID          string    `json:"trust_id"`
	Label            string    `json:"label"`
	CreatedAt        time.Time `json:"created_at"`
	LastUsedAt       time.Time `json:"last_used_at"`
	BackupEligible   bool      `json:"backup_eligible"`
	BackupState      bool      `json:"backup_state"`
	BackupObservedAt time.Time `json:"backup_observed_at"`
}

type operatorUser struct {
	handle      []byte
	credentials []webauthn.Credential
}

func (u operatorUser) WebAuthnID() []byte          { return bytes.Clone(u.handle) }
func (u operatorUser) WebAuthnName() string        { return "Shepherdr operator" }
func (u operatorUser) WebAuthnDisplayName() string { return "Shepherdr operator" }
func (u operatorUser) WebAuthnCredentials() []webauthn.Credential {
	return slices.Clone(u.credentials)
}

func digestToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func decodeDigest(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("invalid token digest")
	}
	return decoded, nil
}

func validateState(state *State) error {
	if state.Version != stateVersion {
		return fmt.Errorf("unsupported access state version %d", state.Version)
	}
	origin, err := ParseCanonicalOrigin(state.PublicOrigin)
	if err != nil || origin.RPID != state.RPID {
		return errors.New("access state has an invalid canonical origin")
	}
	if lifetime, err := ParseSessionLifetime(state.SessionLifetime); err != nil || lifetime.String() != state.SessionLifetime {
		return errors.New("access state has an invalid session lifetime")
	}
	handle, err := base64.RawURLEncoding.DecodeString(state.UserHandle)
	if err != nil || len(handle) != 64 {
		return errors.New("access state has an invalid operator handle")
	}
	if len(state.Credentials) > MaxCredentials || len(state.Invitations) > MaxInvitations || len(state.Sessions) > MaxSessions {
		return errors.New("access state exceeds its record limits")
	}
	trustIDs := map[string]bool{}
	seenTrustIDs := map[string]bool{}
	credentialIDs := map[string]bool{}
	for _, record := range state.Credentials {
		if !validOpaqueID(record.TrustID) || seenTrustIDs[record.TrustID] || len(record.Label) > 160 || record.CreatedAt.IsZero() ||
			record.LastUsedAt.IsZero() || record.BackupObservedAt.IsZero() || record.LastUsedAt.Before(record.CreatedAt) ||
			record.BackupObservedAt.Before(record.CreatedAt) || record.RevokedAt != nil && record.RevokedAt.Before(record.CreatedAt) ||
			len(record.Credential.ID) == 0 || len(record.Credential.PublicKey) == 0 {
			return errors.New("access state has an invalid credential record")
		}
		credentialID := base64.RawURLEncoding.EncodeToString(record.Credential.ID)
		if credentialIDs[credentialID] {
			return errors.New("access state has duplicate credential IDs")
		}
		seenTrustIDs[record.TrustID] = true
		trustIDs[record.TrustID] = record.RevokedAt == nil
		credentialIDs[credentialID] = true
	}
	sessionDigests := map[string]bool{}
	trustSessionCounts := map[string]int{}
	for _, record := range state.Sessions {
		if _, err := decodeDigest(record.Digest); err != nil || sessionDigests[record.Digest] || !trustIDs[record.TrustID] ||
			record.CreatedAt.IsZero() || !record.FreshAt.Equal(record.CreatedAt) || !trustIDs[record.FreshTrustID] {
			return errors.New("access state has an invalid session record")
		}
		lifetime, err := ParseSessionLifetime(record.Lifetime)
		if err != nil || lifetime.None != (record.ExpiresAt == nil) {
			return errors.New("access state has an invalid session expiry")
		}
		if record.ExpiresAt != nil && !record.ExpiresAt.Equal(record.CreatedAt.Add(lifetime.Duration())) {
			return errors.New("access state has a mismatched session expiry")
		}
		sessionDigests[record.Digest] = true
		trustSessionCounts[record.TrustID]++
		if trustSessionCounts[record.TrustID] > MaxTrustSessions {
			return errors.New("access state exceeds the per-passkey session limit")
		}
	}
	invitationDigests := map[string]bool{}
	authorizerInvites := map[string]int{}
	for _, record := range state.Invitations {
		if _, err := decodeDigest(record.Digest); err != nil || invitationDigests[record.Digest] || record.IssuedAt.IsZero() ||
			!record.ExpiresAt.Equal(record.IssuedAt.Add(InvitationLifetime)) {
			return errors.New("access state has an invalid invitation record")
		}
		if record.IssuerTrustID != "" && !trustIDs[record.IssuerTrustID] {
			return errors.New("access state has an inactive invitation issuer")
		}
		if (record.ReservedClientDigest == "") != (record.ReservedUntil == nil) {
			return errors.New("access state has an invalid invitation reservation")
		}
		if record.ReservedClientDigest != "" {
			if _, err := decodeDigest(record.ReservedClientDigest); err != nil || !record.ReservedUntil.After(record.IssuedAt) || record.ReservedUntil.After(record.ExpiresAt) ||
				record.ReservedUntil.After(record.IssuedAt.Add(InvitationLifetime)) {
				return errors.New("access state has an invalid invitation reservation")
			}
		}
		if record.ConsumedAt != nil && (record.ConsumedAt.Before(record.IssuedAt) || record.ConsumedAt.After(record.ExpiresAt) || record.ReservedUntil != nil) {
			return errors.New("access state has an invalid invitation consumption")
		}
		invitationDigests[record.Digest] = true
		if record.ConsumedAt == nil && record.IssuerTrustID != "" {
			authorizerInvites[record.IssuerTrustID]++
			if authorizerInvites[record.IssuerTrustID] > MaxAuthorizerInvites {
				return errors.New("access state exceeds the per-passkey invitation limit")
			}
		}
	}
	return nil
}

func validOpaqueID(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 16
}
