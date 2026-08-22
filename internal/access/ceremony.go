package access

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	CeremonySignIn = "signin"
	CeremonyTrust  = "trust"
	CeremonyReauth = "reauth"
	maxCeremonies  = 128
	maxAttempts    = 5
)

type ceremony struct {
	kind             string
	clientDigest     string
	expiresAt        time.Time
	session          webauthn.SessionData
	invitationDigest string
	label            string
	sessionDigest    string
}

type ceremonyAttempts struct {
	count     int
	expiresAt time.Time
}

type retryableCeremonyError struct {
	cause error
}

func (e *retryableCeremonyError) Error() string { return e.cause.Error() }
func (e *retryableCeremonyError) Unwrap() error { return e.cause }

func CeremonyCanRetry(err error) bool {
	var retryable *retryableCeremonyError
	return errors.As(err, &retryable)
}

type CeremonyBegin struct {
	ClientToken string
	ExpiresAt   time.Time
	Options     any
	TTL         time.Duration
}

type SessionIssue struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt *time.Time
	Lifetime  string
	TrustID   string
	Runtimes  []*sessionRuntime
}

func (m *Manager) BeginSignIn(clientToken string) (CeremonyBegin, error) {
	return m.beginAssertion(CeremonySignIn, clientToken, SessionIdentity{})
}

func (m *Manager) BeginReauthentication(clientToken string, current SessionIdentity) (CeremonyBegin, error) {
	return m.beginAssertion(CeremonyReauth, clientToken, current)
}

func (m *Manager) beginAssertion(kind, clientToken string, current SessionIdentity) (CeremonyBegin, error) {
	now := m.clock.Now()
	clientToken, clientDigest, err := normalizeCeremonyClient(clientToken)
	if err != nil {
		return CeremonyBegin{}, err
	}
	if kind == CeremonyReauth && !m.Recheck(current) {
		return CeremonyBegin{}, ErrUnauthorized
	}
	m.ceremonyMu.Lock()
	defer m.ceremonyMu.Unlock()
	m.pruneCeremoniesLocked(now)
	key := ceremonyKey(kind, clientDigest)
	if attempts := m.attempts[key]; attempts.count >= maxAttempts && now.Before(attempts.expiresAt) {
		return CeremonyBegin{}, ErrUnauthorized
	}
	if _, exists := m.attempts[key]; !exists && len(m.attempts) >= maxCeremonies {
		return CeremonyBegin{}, ErrCapacity
	}
	if _, exists := m.ceremonies[key]; !exists && len(m.ceremonies) >= maxCeremonies {
		return CeremonyBegin{}, ErrCapacity
	}
	options, session, err := m.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return CeremonyBegin{}, err
	}
	expiresAt := now.Add(ReservationLifetime)
	if attempts, ok := m.attempts[key]; ok && now.Before(attempts.expiresAt) {
		expiresAt = attempts.expiresAt
	}
	m.ceremonies[key] = &ceremony{
		kind: kind, clientDigest: clientDigest, expiresAt: expiresAt, session: *session,
		sessionDigest: current.Digest,
	}
	if attempts, ok := m.attempts[key]; !ok || !now.Before(attempts.expiresAt) {
		m.attempts[key] = ceremonyAttempts{expiresAt: expiresAt}
	}
	return CeremonyBegin{ClientToken: clientToken, ExpiresAt: expiresAt, Options: options, TTL: expiresAt.Sub(now)}, nil
}

func (m *Manager) BeginTrust(clientToken, invitationToken, label string) (CeremonyBegin, error) {
	if len(label) > 160 {
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	if label == "" {
		label = "Trusted sign-in"
	}
	now := m.clock.Now()
	clientToken, clientDigest, err := normalizeCeremonyClient(clientToken)
	if err != nil || len(invitationToken) < 40 || len(invitationToken) > 128 {
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	invitationDigest := digestToken(invitationToken)

	m.ceremonyMu.Lock()
	defer m.ceremonyMu.Unlock()
	m.pruneCeremoniesLocked(now)
	key := ceremonyKey(CeremonyTrust, clientDigest)
	if attempts := m.attempts[key]; attempts.count >= maxAttempts && now.Before(attempts.expiresAt) {
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	if _, exists := m.attempts[key]; !exists && len(m.attempts) >= maxCeremonies {
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	if _, exists := m.ceremonies[key]; !exists && len(m.ceremonies) >= maxCeremonies {
		return CeremonyBegin{}, ErrInvitationGeneric
	}

	m.gate.Lock()
	updated, err := cloneState(m.state)
	if err != nil {
		m.gate.Unlock()
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	invitation, ok := usableInvitation(updated, invitationDigest, clientDigest, now)
	if !ok {
		m.gate.Unlock()
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	if invitation.ReservedUntil == nil || invitation.ReservedClientDigest == "" || !now.Before(*invitation.ReservedUntil) {
		reservedUntil := now.Add(ReservationLifetime)
		if reservedUntil.After(invitation.ExpiresAt) {
			reservedUntil = invitation.ExpiresAt
		}
		invitation.ReservedClientDigest = clientDigest
		invitation.ReservedUntil = &reservedUntil
	}
	if err := m.commitLocked(updated); err != nil {
		m.gate.Unlock()
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	user, err := m.activeUserLocked()
	m.gate.Unlock()
	if err != nil {
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(user.credentials))
	for index := range user.credentials {
		exclusions = append(exclusions, user.credentials[index].Descriptor())
	}
	options, session, err := m.webauthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(exclusions),
	)
	if err != nil {
		return CeremonyBegin{}, ErrInvitationGeneric
	}
	expiresAt := *invitation.ReservedUntil
	m.ceremonies[key] = &ceremony{
		kind: CeremonyTrust, clientDigest: clientDigest, expiresAt: expiresAt, session: *session,
		invitationDigest: invitationDigest, label: label,
	}
	if attempts, ok := m.attempts[key]; !ok || !now.Before(attempts.expiresAt) {
		m.attempts[key] = ceremonyAttempts{expiresAt: expiresAt}
	}
	return CeremonyBegin{ClientToken: clientToken, ExpiresAt: expiresAt, Options: options, TTL: expiresAt.Sub(now)}, nil
}

func (m *Manager) FinishSignIn(clientToken string, request *http.Request, old *SessionIdentity) (SessionIssue, error) {
	ceremony, err := m.takeCeremony(CeremonySignIn, clientToken)
	if err != nil {
		return SessionIssue{}, ErrUnauthorized
	}
	credential, trustID, err := m.verifyAssertion(ceremony, request)
	if err != nil {
		if m.recordFailure(ceremony) {
			return SessionIssue{}, &retryableCeremonyError{cause: ErrUnauthorized}
		}
		return SessionIssue{}, ErrUnauthorized
	}
	oldDigest := ""
	if old != nil {
		oldDigest = old.Digest
	}
	issue, err := m.commitAssertion(credential, trustID, oldDigest, false)
	if err != nil {
		return SessionIssue{}, ErrUnauthorized
	}
	m.clearAttempts(ceremony)
	return issue, nil
}

func (m *Manager) FinishReauthentication(clientToken string, request *http.Request, current SessionIdentity) (SessionIssue, error) {
	ceremony, err := m.takeCeremony(CeremonyReauth, clientToken)
	if err != nil || ceremony.sessionDigest != current.Digest {
		return SessionIssue{}, ErrUnauthorized
	}
	credential, trustID, err := m.verifyAssertion(ceremony, request)
	if err != nil {
		if m.recordFailure(ceremony) {
			return SessionIssue{}, &retryableCeremonyError{cause: ErrUnauthorized}
		}
		return SessionIssue{}, ErrUnauthorized
	}
	issue, err := m.commitAssertion(credential, trustID, current.Digest, true)
	if err != nil {
		return SessionIssue{}, ErrUnauthorized
	}
	m.clearAttempts(ceremony)
	return issue, nil
}

func (m *Manager) FinishTrust(clientToken string, request *http.Request, old *SessionIdentity) (SessionIssue, error) {
	ceremony, err := m.takeCeremony(CeremonyTrust, clientToken)
	if err != nil {
		return SessionIssue{}, ErrInvitationGeneric
	}
	m.gate.RLock()
	user, userErr := m.activeUserLocked()
	m.gate.RUnlock()
	if userErr != nil {
		return SessionIssue{}, ErrInvitationGeneric
	}
	credential, err := m.webauthn.FinishRegistration(user, ceremony.session, request)
	if err != nil {
		if m.recordFailure(ceremony) {
			return SessionIssue{}, &retryableCeremonyError{cause: ErrInvitationGeneric}
		}
		return SessionIssue{}, ErrInvitationGeneric
	}
	oldDigest := ""
	if old != nil {
		oldDigest = old.Digest
	}
	issue, err := m.commitRegistration(ceremony, credential, oldDigest)
	if err != nil {
		return SessionIssue{}, ErrInvitationGeneric
	}
	m.clearAttempts(ceremony)
	return issue, nil
}

func (m *Manager) takeCeremony(kind, clientToken string) (*ceremony, error) {
	if len(clientToken) < 40 || len(clientToken) > 128 {
		return nil, ErrUnauthorized
	}
	clientDigest := digestToken(clientToken)
	key := ceremonyKey(kind, clientDigest)
	m.ceremonyMu.Lock()
	defer m.ceremonyMu.Unlock()
	now := m.clock.Now()
	m.pruneCeremoniesLocked(now)
	current := m.ceremonies[key]
	if current == nil || !now.Before(current.expiresAt) {
		return nil, ErrUnauthorized
	}
	delete(m.ceremonies, key)
	return current, nil
}

func (m *Manager) verifyAssertion(ceremony *ceremony, request *http.Request) (*webauthn.Credential, string, error) {
	var actualTrustID string
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		m.gate.RLock()
		defer m.gate.RUnlock()
		user, trustID, err := m.discoverableUserLocked(rawID, userHandle)
		if err != nil {
			return nil, err
		}
		actualTrustID = trustID
		return user, nil
	}
	credential, err := m.webauthn.FinishDiscoverableLogin(handler, ceremony.session, request)
	if err != nil || actualTrustID == "" {
		return nil, "", ErrUnauthorized
	}
	return credential, actualTrustID, nil
}

func (m *Manager) commitAssertion(credential *webauthn.Credential, trustID, oldDigest string, reauthentication bool) (SessionIssue, error) {
	m.gate.Lock()
	defer m.gate.Unlock()
	now := m.clock.Now()
	if reauthentication {
		if _, ok := m.activeSessionLocked(oldDigest, now); !ok {
			return SessionIssue{}, ErrUnauthorized
		}
	}
	updated, err := cloneState(m.state)
	if err != nil {
		return SessionIssue{}, err
	}
	found := false
	for index := range updated.Credentials {
		record := &updated.Credentials[index]
		if record.TrustID == trustID && record.RevokedAt == nil && bytes.Equal(record.Credential.ID, credential.ID) {
			record.Credential = *credential
			record.LastUsedAt = now
			record.BackupObservedAt = now
			found = true
			break
		}
	}
	if !found {
		return SessionIssue{}, ErrUnauthorized
	}
	issue, invalidated, err := m.addSession(updated, trustID, trustID, now, oldDigest)
	if err != nil {
		return SessionIssue{}, err
	}
	if err := m.commitLocked(updated); err != nil {
		return SessionIssue{}, err
	}
	issue.Runtimes = m.cancelRuntimes(invalidated)
	m.installRuntime(digestToken(issue.Token))
	return issue, nil
}

func (m *Manager) commitRegistration(ceremony *ceremony, credential *webauthn.Credential, oldDigest string) (SessionIssue, error) {
	m.gate.Lock()
	defer m.gate.Unlock()
	now := m.clock.Now()
	updated, err := cloneState(m.state)
	if err != nil {
		return SessionIssue{}, err
	}
	invitation, ok := usableInvitation(updated, ceremony.invitationDigest, ceremony.clientDigest, now)
	if !ok || invitation.ReservedClientDigest != ceremony.clientDigest || invitation.ReservedUntil == nil || !now.Before(*invitation.ReservedUntil) {
		return SessionIssue{}, ErrInvitationGeneric
	}
	updated.Credentials = slices.DeleteFunc(updated.Credentials, func(record CredentialRecord) bool {
		return record.RevokedAt != nil
	})
	if activeCredentialCount(updated) >= MaxCredentials {
		return SessionIssue{}, ErrCapacity
	}
	for _, record := range updated.Credentials {
		if bytes.Equal(record.Credential.ID, credential.ID) {
			return SessionIssue{}, ErrInvitationGeneric
		}
	}
	trustID, err := randomToken(16)
	if err != nil {
		return SessionIssue{}, err
	}
	consumed := now
	invitation.ConsumedAt = &consumed
	invitation.ReservedClientDigest = ""
	invitation.ReservedUntil = nil
	updated.Credentials = append(updated.Credentials, CredentialRecord{
		TrustID: trustID, Label: ceremony.label, CreatedAt: now, LastUsedAt: now, BackupObservedAt: now,
		Credential: *credential,
	})
	issue, invalidated, err := m.addSession(updated, trustID, trustID, now, oldDigest)
	if err != nil {
		return SessionIssue{}, err
	}
	if err := m.commitLocked(updated); err != nil {
		return SessionIssue{}, err
	}
	issue.Runtimes = m.cancelRuntimes(invalidated)
	m.installRuntime(digestToken(issue.Token))
	return issue, nil
}

func (m *Manager) addSession(state *State, trustID, freshTrustID string, now time.Time, replaceDigest string) (SessionIssue, []string, error) {
	token, err := randomToken(32)
	if err != nil {
		return SessionIssue{}, nil, err
	}
	lifetime, err := ParseSessionLifetime(state.SessionLifetime)
	if err != nil {
		return SessionIssue{}, nil, err
	}
	digest := digestToken(token)
	var invalidated []string
	if replaceDigest != "" {
		state.Sessions = slices.DeleteFunc(state.Sessions, func(record SessionRecord) bool {
			if record.Digest == replaceDigest {
				invalidated = append(invalidated, record.Digest)
				return true
			}
			return false
		})
	}
	for trustSessionCount(state, trustID) >= MaxTrustSessions {
		index := oldestSessionIndex(state, trustID)
		invalidated = append(invalidated, state.Sessions[index].Digest)
		state.Sessions = append(state.Sessions[:index], state.Sessions[index+1:]...)
	}
	for len(state.Sessions) >= MaxSessions {
		index := oldestSessionIndex(state, "")
		invalidated = append(invalidated, state.Sessions[index].Digest)
		state.Sessions = append(state.Sessions[:index], state.Sessions[index+1:]...)
	}
	record := SessionRecord{
		Digest: digest, TrustID: trustID, CreatedAt: now, Lifetime: lifetime.String(),
		FreshTrustID: freshTrustID, FreshAt: now,
	}
	if !lifetime.None {
		expires := now.Add(lifetime.Duration())
		record.ExpiresAt = &expires
	}
	state.Sessions = append(state.Sessions, record)
	return SessionIssue{
		Token: token, CreatedAt: now, ExpiresAt: record.ExpiresAt, Lifetime: record.Lifetime, TrustID: trustID,
	}, invalidated, nil
}

func usableInvitation(state *State, invitationDigest, clientDigest string, now time.Time) (*InvitationRecord, bool) {
	for index := range state.Invitations {
		record := &state.Invitations[index]
		if record.Digest != invitationDigest || record.ConsumedAt != nil || !now.Before(record.ExpiresAt) {
			continue
		}
		if record.IssuerTrustID != "" {
			active := false
			for _, credential := range state.Credentials {
				if credential.TrustID == record.IssuerTrustID && credential.RevokedAt == nil {
					active = true
					break
				}
			}
			if !active {
				return nil, false
			}
		}
		if record.ReservedUntil != nil && now.Before(*record.ReservedUntil) && record.ReservedClientDigest != clientDigest {
			return nil, false
		}
		if record.ReservedUntil != nil && !now.Before(*record.ReservedUntil) {
			record.ReservedUntil = nil
			record.ReservedClientDigest = ""
		}
		return record, true
	}
	return nil, false
}

func trustSessionCount(state *State, trustID string) int {
	count := 0
	for _, record := range state.Sessions {
		if record.TrustID == trustID {
			count++
		}
	}
	return count
}

func oldestSessionIndex(state *State, trustID string) int {
	index := -1
	for candidate := range state.Sessions {
		if trustID != "" && state.Sessions[candidate].TrustID != trustID {
			continue
		}
		if index == -1 || state.Sessions[candidate].CreatedAt.Before(state.Sessions[index].CreatedAt) {
			index = candidate
		}
	}
	return index
}

func normalizeCeremonyClient(token string) (string, string, error) {
	if token != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err == nil && len(decoded) == 32 {
			return token, digestToken(token), nil
		}
	}
	token, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	return token, digestToken(token), nil
}

func ceremonyKey(kind, clientDigest string) string { return kind + ":" + clientDigest }

func (m *Manager) pruneCeremoniesLocked(now time.Time) {
	for key, current := range m.ceremonies {
		if !now.Before(current.expiresAt) {
			delete(m.ceremonies, key)
		}
	}
	for key, attempts := range m.attempts {
		if !now.Before(attempts.expiresAt) {
			delete(m.attempts, key)
		}
	}
}

func (m *Manager) recordFailure(current *ceremony) bool {
	m.ceremonyMu.Lock()
	defer m.ceremonyMu.Unlock()
	key := ceremonyKey(current.kind, current.clientDigest)
	attempts := m.attempts[key]
	attempts.count++
	if attempts.expiresAt.IsZero() {
		attempts.expiresAt = current.expiresAt
	}
	m.attempts[key] = attempts
	return attempts.count < maxAttempts && m.clock.Now().Before(attempts.expiresAt)
}

func (m *Manager) clearAttempts(current *ceremony) {
	m.ceremonyMu.Lock()
	delete(m.attempts, ceremonyKey(current.kind, current.clientDigest))
	m.ceremonyMu.Unlock()
}
