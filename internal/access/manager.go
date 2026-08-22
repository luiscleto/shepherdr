package access

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var (
	ErrUnauthorized      = errors.New("sign in is required")
	ErrFreshRequired     = errors.New("use a passkey before changing trusted sign-ins")
	ErrLastCredential    = errors.New("the final trusted sign-in cannot be revoked; stop Shepherdr and use access reset")
	ErrInvitationGeneric = errors.New("this invitation cannot be used")
	ErrCapacity          = errors.New("access record limit reached")
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type SessionIdentity struct {
	Digest  string
	TrustID string
}

type SessionLease struct {
	manager *Manager
	runtime *sessionRuntime
	session SessionIdentity
	once    sync.Once
}

func (l *SessionLease) Context() context.Context { return l.runtime.context }
func (l *SessionLease) Session() SessionIdentity { return l.session }
func (l *SessionLease) Valid() bool {
	select {
	case <-l.runtime.context.Done():
		return false
	default:
		return l.manager.Recheck(l.session)
	}
}
func (l *SessionLease) Close() {
	l.once.Do(func() { l.runtime.wait.Done() })
}

type sessionRuntime struct {
	context context.Context
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

type NotificationAuthority interface {
	RemoveTrustSubscriptions(string) error
	ResetProtectedSubscriptions() error
}

type Manager struct {
	gate          sync.RWMutex
	store         *Store
	state         *State
	origin        Origin
	webauthn      *webauthn.WebAuthn
	clock         Clock
	authority     NotificationAuthority
	ceremonyMu    sync.Mutex
	ceremonies    map[string]*ceremony
	attempts      map[string]ceremonyAttempts
	ceremonyHooks *ceremonyVerificationHooks

	runtimeMu sync.Mutex
	runtimes  map[string]*sessionRuntime
	stop      chan struct{}
	stopped   chan struct{}
}

func NewManager(store *Store, clock Clock) (*Manager, error) {
	if store == nil || store.state == nil {
		return nil, errors.New("access store is required")
	}
	if clock == nil {
		clock = realClock{}
	}
	origin, err := ParseCanonicalOrigin(store.state.PublicOrigin)
	if err != nil {
		return nil, err
	}
	verificationRequired := protocol.VerificationRequired
	wa, err := webauthn.New(&webauthn.Config{
		RPID: origin.RPID, RPDisplayName: "Shepherdr", RPOrigins: []string{origin.Value},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   verificationRequired,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("configure WebAuthn: %w", err)
	}
	manager := &Manager{
		store: store, state: store.state, origin: origin, webauthn: wa, clock: clock,
		runtimes: map[string]*sessionRuntime{}, ceremonies: map[string]*ceremony{},
		attempts: map[string]ceremonyAttempts{}, stop: make(chan struct{}), stopped: make(chan struct{}),
	}
	for _, record := range manager.state.Sessions {
		manager.runtimes[record.Digest] = newSessionRuntime()
	}
	if _, err := manager.expireNow(); err != nil {
		return nil, err
	}
	go manager.expiryLoop()
	return manager, nil
}

func (m *Manager) Close() {
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	<-m.stopped
	m.runtimeMu.Lock()
	runtimes := make([]*sessionRuntime, 0, len(m.runtimes))
	for _, runtime := range m.runtimes {
		runtime.cancel()
		runtimes = append(runtimes, runtime)
	}
	m.runtimeMu.Unlock()
	for _, runtime := range runtimes {
		runtime.wait.Wait()
	}
}

func (m *Manager) SetNotificationAuthority(authority NotificationAuthority) {
	m.gate.Lock()
	defer m.gate.Unlock()
	m.authority = authority
}

func (m *Manager) Origin() Origin { return m.origin }

func (m *Manager) ProtectedState() (hasCredentials bool, lifetime string) {
	m.gate.RLock()
	defer m.gate.RUnlock()
	return activeCredentialCount(m.state) > 0, m.state.SessionLifetime
}

func (m *Manager) AuthenticateToken(token string) (SessionIdentity, bool) {
	if len(token) < 40 || len(token) > 128 {
		return SessionIdentity{}, false
	}
	digest := digestToken(token)
	m.gate.RLock()
	record, ok := m.activeSessionLocked(digest, m.clock.Now())
	m.gate.RUnlock()
	if !ok {
		return SessionIdentity{}, false
	}
	return SessionIdentity{Digest: digest, TrustID: record.TrustID}, true
}

func (m *Manager) Recheck(session SessionIdentity) bool {
	m.gate.RLock()
	defer m.gate.RUnlock()
	record, ok := m.activeSessionLocked(session.Digest, m.clock.Now())
	return ok && record.TrustID == session.TrustID
}

func (m *Manager) Bind(session SessionIdentity) (*SessionLease, bool) {
	m.gate.RLock()
	record, ok := m.activeSessionLocked(session.Digest, m.clock.Now())
	if !ok || record.TrustID != session.TrustID {
		m.gate.RUnlock()
		return nil, false
	}
	m.runtimeMu.Lock()
	runtime := m.runtimes[session.Digest]
	if runtime != nil {
		runtime.wait.Add(1)
	}
	m.runtimeMu.Unlock()
	m.gate.RUnlock()
	if runtime == nil {
		return nil, false
	}
	return &SessionLease{manager: m, runtime: runtime, session: session}, true
}

func (m *Manager) WithSessionRead(session SessionIdentity, operation func(trustID string) error) error {
	m.gate.RLock()
	defer m.gate.RUnlock()
	record, ok := m.activeSessionLocked(session.Digest, m.clock.Now())
	if !ok || record.TrustID != session.TrustID || !m.activeTrustLocked(record.TrustID) {
		return ErrUnauthorized
	}
	return operation(record.TrustID)
}

func (m *Manager) WithSessionCommit(session SessionIdentity, operation func(trustID string) error) error {
	m.gate.Lock()
	defer m.gate.Unlock()
	record, ok := m.activeSessionLocked(session.Digest, m.clock.Now())
	if !ok || record.TrustID != session.TrustID || !m.activeTrustLocked(record.TrustID) {
		return ErrUnauthorized
	}
	return operation(record.TrustID)
}

func (m *Manager) WithActiveTrustRead(trustID string, operation func() error) error {
	m.gate.RLock()
	defer m.gate.RUnlock()
	if !m.activeTrustLocked(trustID) {
		return ErrUnauthorized
	}
	return operation()
}

func (m *Manager) Devices(session SessionIdentity) ([]Device, error) {
	m.gate.RLock()
	defer m.gate.RUnlock()
	if record, ok := m.activeSessionLocked(session.Digest, m.clock.Now()); !ok || record.TrustID != session.TrustID {
		return nil, ErrUnauthorized
	}
	devices := make([]Device, 0, activeCredentialCount(m.state))
	for _, record := range m.state.Credentials {
		if record.RevokedAt != nil {
			continue
		}
		devices = append(devices, Device{
			TrustID: record.TrustID, Label: record.Label, CreatedAt: record.CreatedAt, LastUsedAt: record.LastUsedAt,
			BackupEligible: record.Credential.Flags.BackupEligible, BackupState: record.Credential.Flags.BackupState,
			BackupObservedAt: record.BackupObservedAt,
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].CreatedAt.Before(devices[j].CreatedAt) })
	return devices, nil
}

func (m *Manager) LocalDevices() []Device {
	m.gate.RLock()
	defer m.gate.RUnlock()
	devices := make([]Device, 0, activeCredentialCount(m.state))
	for _, record := range m.state.Credentials {
		if record.RevokedAt != nil {
			continue
		}
		devices = append(devices, Device{
			TrustID: record.TrustID, Label: record.Label, CreatedAt: record.CreatedAt, LastUsedAt: record.LastUsedAt,
			BackupEligible: record.Credential.Flags.BackupEligible, BackupState: record.Credential.Flags.BackupState,
			BackupObservedAt: record.BackupObservedAt,
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].CreatedAt.Before(devices[j].CreatedAt) })
	return devices
}

func (m *Manager) CreateBrowserInvitation(session SessionIdentity) (string, time.Time, error) {
	m.gate.Lock()
	defer m.gate.Unlock()
	now := m.clock.Now()
	sessionRecord, ok := m.activeSessionLocked(session.Digest, now)
	if !ok || sessionRecord.TrustID != session.TrustID {
		return "", time.Time{}, ErrUnauthorized
	}
	if now.Sub(sessionRecord.FreshAt) > FreshLifetime || !m.activeTrustLocked(sessionRecord.FreshTrustID) {
		return "", time.Time{}, ErrFreshRequired
	}
	updated, err := cloneState(m.state)
	if err != nil {
		return "", time.Time{}, err
	}
	pruneInvitations(updated, now)
	activeForAuthorizer := 0
	for _, invitation := range updated.Invitations {
		if invitation.ConsumedAt == nil && invitation.IssuerTrustID == sessionRecord.FreshTrustID {
			activeForAuthorizer++
		}
	}
	if len(updated.Invitations) >= MaxInvitations || activeForAuthorizer >= MaxAuthorizerInvites {
		return "", time.Time{}, ErrCapacity
	}
	token, invitation, err := newInvitation(now, sessionRecord.FreshTrustID)
	if err != nil {
		return "", time.Time{}, err
	}
	updated.Invitations = append(updated.Invitations, invitation)
	if err := m.commitLocked(updated); err != nil {
		return "", time.Time{}, err
	}
	return m.origin.InvitationURL(token), invitation.ExpiresAt, nil
}

func (m *Manager) CreateLocalInvitation() (string, time.Time, error) {
	m.gate.Lock()
	defer m.gate.Unlock()
	now := m.clock.Now()
	updated, err := cloneState(m.state)
	if err != nil {
		return "", time.Time{}, err
	}
	pruneInvitations(updated, now)
	if len(updated.Invitations) >= MaxInvitations {
		return "", time.Time{}, ErrCapacity
	}
	token, invitation, err := newInvitation(now, "")
	if err != nil {
		return "", time.Time{}, err
	}
	updated.Invitations = append(updated.Invitations, invitation)
	if err := m.commitLocked(updated); err != nil {
		return "", time.Time{}, err
	}
	return m.origin.InvitationURL(token), invitation.ExpiresAt, nil
}

func (m *Manager) RevokeBrowser(session SessionIdentity, targetTrustID string) ([]*sessionRuntime, error) {
	m.gate.Lock()
	now := m.clock.Now()
	sessionRecord, ok := m.activeSessionLocked(session.Digest, now)
	if !ok || sessionRecord.TrustID != session.TrustID {
		m.gate.Unlock()
		return nil, ErrUnauthorized
	}
	if now.Sub(sessionRecord.FreshAt) > FreshLifetime || !m.activeTrustLocked(sessionRecord.FreshTrustID) {
		m.gate.Unlock()
		return nil, ErrFreshRequired
	}
	runtimes, err := m.revokeLocked(targetTrustID, now)
	m.gate.Unlock()
	return runtimes, err
}

func (m *Manager) RevokeLocal(targetTrustID string) ([]*sessionRuntime, error) {
	m.gate.Lock()
	runtimes, err := m.revokeLocked(targetTrustID, m.clock.Now())
	m.gate.Unlock()
	return runtimes, err
}

func (m *Manager) revokeLocked(targetTrustID string, now time.Time) ([]*sessionRuntime, error) {
	if activeCredentialCount(m.state) <= 1 {
		return nil, ErrLastCredential
	}
	found := false
	updated, err := cloneState(m.state)
	if err != nil {
		return nil, err
	}
	for index := range updated.Credentials {
		if updated.Credentials[index].TrustID == targetTrustID && updated.Credentials[index].RevokedAt == nil {
			revoked := now.UTC()
			updated.Credentials[index].RevokedAt = &revoked
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("trusted sign-in not found")
	}
	var invalidated []string
	updated.Sessions = slices.DeleteFunc(updated.Sessions, func(record SessionRecord) bool {
		if record.TrustID == targetTrustID {
			invalidated = append(invalidated, record.Digest)
			return true
		}
		return false
	})
	updated.Invitations = slices.DeleteFunc(updated.Invitations, func(record InvitationRecord) bool {
		return record.IssuerTrustID == targetTrustID
	})
	if err := m.commitLocked(updated); err != nil {
		return nil, err
	}
	runtimes := m.cancelRuntimes(invalidated)
	if m.authority != nil {
		_ = m.authority.RemoveTrustSubscriptions(targetTrustID)
	}
	return runtimes, nil
}

func (m *Manager) SignOut(session SessionIdentity) ([]*sessionRuntime, error) {
	m.gate.Lock()
	updated, err := cloneState(m.state)
	if err != nil {
		m.gate.Unlock()
		return nil, err
	}
	found := false
	updated.Sessions = slices.DeleteFunc(updated.Sessions, func(record SessionRecord) bool {
		if record.Digest == session.Digest && record.TrustID == session.TrustID {
			found = true
			return true
		}
		return false
	})
	if !found {
		m.gate.Unlock()
		return nil, ErrUnauthorized
	}
	if err := m.commitLocked(updated); err != nil {
		m.gate.Unlock()
		return nil, err
	}
	runtimes := m.cancelRuntimes([]string{session.Digest})
	m.gate.Unlock()
	return runtimes, nil
}

func (m *Manager) Reset() ([]*sessionRuntime, error) {
	m.gate.Lock()
	updated, err := cloneState(m.state)
	if err != nil {
		m.gate.Unlock()
		return nil, err
	}
	handle, err := randomBytes(64)
	if err != nil {
		m.gate.Unlock()
		return nil, err
	}
	invalidated := make([]string, 0, len(updated.Sessions))
	for _, record := range updated.Sessions {
		invalidated = append(invalidated, record.Digest)
	}
	updated.UserHandle = base64.RawURLEncoding.EncodeToString(handle)
	updated.Credentials = []CredentialRecord{}
	updated.Sessions = []SessionRecord{}
	updated.Invitations = []InvitationRecord{}
	if err := m.commitLocked(updated); err != nil {
		m.gate.Unlock()
		return nil, err
	}
	runtimes := m.cancelRuntimes(invalidated)
	if m.authority != nil {
		_ = m.authority.ResetProtectedSubscriptions()
	}
	m.gate.Unlock()
	return runtimes, nil
}

func WaitRuntimes(runtimes []*sessionRuntime) {
	for _, runtime := range runtimes {
		runtime.wait.Wait()
	}
}

func (m *Manager) ActiveTrustIDs() map[string]struct{} {
	m.gate.RLock()
	defer m.gate.RUnlock()
	result := map[string]struct{}{}
	for _, record := range m.state.Credentials {
		if record.RevokedAt == nil {
			result[record.TrustID] = struct{}{}
		}
	}
	return result
}

func (m *Manager) activeSessionLocked(digest string, now time.Time) (SessionRecord, bool) {
	for _, record := range m.state.Sessions {
		if record.Digest != digest || record.ExpiresAt != nil && !now.Before(*record.ExpiresAt) || !m.activeTrustLocked(record.TrustID) {
			continue
		}
		return record, true
	}
	return SessionRecord{}, false
}

func (m *Manager) activeTrustLocked(trustID string) bool {
	for _, record := range m.state.Credentials {
		if record.TrustID == trustID {
			return record.RevokedAt == nil
		}
	}
	return false
}

func (m *Manager) activeUserLocked() (operatorUser, error) {
	handle, err := base64.RawURLEncoding.DecodeString(m.state.UserHandle)
	if err != nil {
		return operatorUser{}, err
	}
	credentials := make([]webauthn.Credential, 0, activeCredentialCount(m.state))
	for _, record := range m.state.Credentials {
		if record.RevokedAt == nil {
			credentials = append(credentials, record.Credential)
		}
	}
	return operatorUser{handle: handle, credentials: credentials}, nil
}

func (m *Manager) discoverableUserLocked(rawID, userHandle []byte) (operatorUser, string, error) {
	user, err := m.activeUserLocked()
	if err != nil || !bytes.Equal(user.handle, userHandle) {
		return operatorUser{}, "", ErrUnauthorized
	}
	for _, record := range m.state.Credentials {
		if record.RevokedAt == nil && bytes.Equal(record.Credential.ID, rawID) {
			return user, record.TrustID, nil
		}
	}
	return operatorUser{}, "", ErrUnauthorized
}

func (m *Manager) commitLocked(updated *State) error {
	if err := validateState(updated); err != nil {
		return err
	}
	original := m.store.state
	m.store.state = updated
	if err := m.store.write(); err != nil {
		m.store.state = original
		return err
	}
	m.state = updated
	return nil
}

func (m *Manager) cancelRuntimes(digests []string) []*sessionRuntime {
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	runtimes := make([]*sessionRuntime, 0, len(digests))
	for _, digest := range digests {
		runtime := m.runtimes[digest]
		if runtime == nil {
			continue
		}
		delete(m.runtimes, digest)
		runtime.cancel()
		runtimes = append(runtimes, runtime)
	}
	return runtimes
}

func (m *Manager) installRuntime(digest string) {
	m.runtimeMu.Lock()
	m.runtimes[digest] = newSessionRuntime()
	m.runtimeMu.Unlock()
}

func newSessionRuntime() *sessionRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &sessionRuntime{context: ctx, cancel: cancel}
}

func cloneState(state *State) (*State, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	var clone State
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func pruneInvitations(state *State, now time.Time) {
	state.Invitations = slices.DeleteFunc(state.Invitations, func(record InvitationRecord) bool {
		return record.ConsumedAt != nil || !now.Before(record.ExpiresAt)
	})
}

func (m *Manager) expireNow() ([]*sessionRuntime, error) {
	m.gate.Lock()
	defer m.gate.Unlock()
	now := m.clock.Now()
	updated, err := cloneState(m.state)
	if err != nil {
		return nil, err
	}
	var invalidated []string
	updated.Sessions = slices.DeleteFunc(updated.Sessions, func(record SessionRecord) bool {
		if record.ExpiresAt != nil && !now.Before(*record.ExpiresAt) {
			invalidated = append(invalidated, record.Digest)
			return true
		}
		return false
	})
	if len(invalidated) == 0 {
		return nil, nil
	}
	if err := m.commitLocked(updated); err != nil {
		return nil, err
	}
	return m.cancelRuntimes(invalidated), nil
}

func (m *Manager) expiryLoop() {
	defer close(m.stopped)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			runtimes, _ := m.expireNow()
			WaitRuntimes(runtimes)
		}
	}
}
