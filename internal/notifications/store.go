package notifications

import (
	"bytes"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const maxStateBytes = 1 << 20

type persistedState struct {
	Version         int            `json:"version"`
	VAPIDContact    string         `json:"vapid_contact"`
	VAPIDPrivateKey string         `json:"vapid_private_key"`
	VAPIDPublicKey  string         `json:"vapid_public_key"`
	Subscriptions   []Subscription `json:"subscriptions"`
}

type Store struct {
	path string

	mu    sync.RWMutex
	state *persistedState
	err   error
}

func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(directory, "shepherdr", "notifications.json"), nil
}

func OpenStore(path, suppliedContact string) (*Store, error) {
	store := &Store{path: path}
	if suppliedContact != "" {
		validated, err := ValidateContact(suppliedContact)
		if err != nil {
			store.err = err
			return store, err
		}
		suppliedContact = validated
	}
	state, err := readState(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist) && suppliedContact == "":
		return store, nil
	case errors.Is(err, os.ErrNotExist):
		privateKey, publicKey, keyErr := webpush.GenerateVAPIDKeys()
		if keyErr != nil {
			store.err = fmt.Errorf("generate notification keys: %w", keyErr)
			return store, store.err
		}
		state = &persistedState{
			Version:         stateVersion,
			VAPIDContact:    suppliedContact,
			VAPIDPrivateKey: privateKey,
			VAPIDPublicKey:  publicKey,
			Subscriptions:   []Subscription{},
		}
		if err := writeState(path, state); err != nil {
			store.err = err
			return store, err
		}
	default:
		store.err = err
		return store, err
	}

	store.state = state
	if state != nil && state.Version != stateVersion {
		updated := cloneState(state)
		updated.Version = stateVersion
		for index := range updated.Subscriptions {
			updated.Subscriptions[index].Events.TrustedSignInAdded = true
			updated.Subscriptions[index].Events.TrustedSignInRemoved = true
		}
		if err := validateState(updated); err != nil {
			store.err = err
			store.state = nil
			return store, err
		}
		if err := writeState(path, updated); err != nil {
			store.err = err
			store.state = nil
			return store, err
		}
		store.state = updated
		state = updated
	}
	if suppliedContact != "" && state.VAPIDContact != suppliedContact {
		updated := cloneState(state)
		updated.VAPIDContact = suppliedContact
		if err := writeState(path, updated); err != nil {
			store.err = err
			store.state = nil
			return store, err
		}
		store.state = updated
	}
	return store, nil
}

func UnavailableStore(err error) *Store {
	return &Store{err: err}
}

func ValidateContact(value string) (string, error) {
	if len(value) == 0 || len(value) > 2048 || strings.TrimSpace(value) != value {
		return "", errors.New("-vapid-contact must be a real mailto: or HTTPS URI")
	}
	if strings.HasPrefix(value, "mailto:") {
		address := strings.TrimPrefix(value, "mailto:")
		if address == "" || strings.ContainsAny(address, "?#") {
			return "", errors.New("-vapid-contact must contain one valid mailto: address")
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || !strings.Contains(address, "@") {
			return "", errors.New("-vapid-contact must contain one valid mailto: address")
		}
		return value, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("-vapid-contact must be an absolute HTTPS URI without credentials or a fragment")
	}
	return parsed.String(), nil
}

func (s *Store) Config() (contact, publicKey string, configured bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return "", "", false, s.err
	}
	if s.state == nil {
		return "", "", false, nil
	}
	return s.state.VAPIDContact, s.state.VAPIDPublicKey, true, nil
}

func (s *Store) deliveryState() (string, string, string, []Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil || s.state == nil {
		return "", "", "", nil, false
	}
	return s.state.VAPIDContact, s.state.VAPIDPublicKey, s.state.VAPIDPrivateKey, append([]Subscription(nil), s.state.Subscriptions...), true
}

func (s *Store) Lookup(endpoint string) (EventSettings, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return EventSettings{}, false, s.err
	}
	if s.state == nil {
		return DefaultEventSettings(), false, nil
	}
	for _, subscription := range s.state.Subscriptions {
		if subscription.Endpoint == endpoint {
			return subscription.Events, true, nil
		}
	}
	return DefaultEventSettings(), false, nil
}

func (s *Store) LookupOwned(endpoint, trustID string) (EventSettings, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return EventSettings{}, false, s.err
	}
	if s.state == nil {
		return DefaultEventSettings(), false, nil
	}
	for _, subscription := range s.state.Subscriptions {
		if subscription.Endpoint == endpoint && subscription.TrustID == trustID {
			return subscription.Events, true, nil
		}
	}
	return DefaultEventSettings(), false, nil
}

func (s *Store) Upsert(subscription Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.state == nil {
		return errors.New("notifications are not configured")
	}
	updated := cloneState(s.state)
	for index := range updated.Subscriptions {
		if updated.Subscriptions[index].Endpoint == subscription.Endpoint {
			updated.Subscriptions[index] = subscription
			if err := writeState(s.path, updated); err != nil {
				return err
			}
			s.state = updated
			return nil
		}
	}
	if len(updated.Subscriptions) >= MaxSubscriptions {
		return fmt.Errorf("notification subscription limit reached")
	}
	updated.Subscriptions = append(updated.Subscriptions, subscription)
	if err := writeState(s.path, updated); err != nil {
		return err
	}
	s.state = updated
	return nil
}

func (s *Store) EnableProtected(activeTrustIDs map[string]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.state == nil {
		return nil
	}
	updated := cloneState(s.state)
	updated.Version = stateVersion
	kept := updated.Subscriptions[:0]
	for _, subscription := range updated.Subscriptions {
		if _, active := activeTrustIDs[subscription.TrustID]; active && subscription.TrustID != "" {
			kept = append(kept, subscription)
		}
	}
	updated.Subscriptions = kept
	if err := writeState(s.path, updated); err != nil {
		return err
	}
	s.state = updated
	return nil
}

func (s *Store) UpsertOwned(subscription Subscription, trustID string) error {
	if trustID == "" {
		return errors.New("notification subscription owner is required")
	}
	subscription.TrustID = trustID
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.state == nil {
		return errors.New("notifications are not configured")
	}
	updated := cloneState(s.state)
	for index := range updated.Subscriptions {
		if updated.Subscriptions[index].Endpoint != subscription.Endpoint {
			continue
		}
		if updated.Subscriptions[index].TrustID != trustID {
			return errors.New("notification subscription belongs to another trusted sign-in")
		}
		updated.Subscriptions[index] = subscription
		if err := writeState(s.path, updated); err != nil {
			return err
		}
		s.state = updated
		return nil
	}
	if len(updated.Subscriptions) >= MaxSubscriptions {
		return errors.New("notification subscription limit reached")
	}
	updated.Subscriptions = append(updated.Subscriptions, subscription)
	if err := writeState(s.path, updated); err != nil {
		return err
	}
	s.state = updated
	return nil
}

func (s *Store) RemoveOwned(endpoint, trustID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	if s.state == nil {
		return false, nil
	}
	updated := cloneState(s.state)
	for index, subscription := range updated.Subscriptions {
		if subscription.Endpoint != endpoint || subscription.TrustID != trustID {
			continue
		}
		updated.Subscriptions = append(updated.Subscriptions[:index], updated.Subscriptions[index+1:]...)
		if err := writeState(s.path, updated); err != nil {
			return false, err
		}
		s.state = updated
		return true, nil
	}
	return false, nil
}

func (s *Store) RemoveTrustSubscriptions(trustID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.state == nil {
		return nil
	}
	updated := cloneState(s.state)
	kept := updated.Subscriptions[:0]
	for _, subscription := range updated.Subscriptions {
		if subscription.TrustID != trustID {
			kept = append(kept, subscription)
		}
	}
	updated.Subscriptions = kept
	if len(updated.Subscriptions) == len(s.state.Subscriptions) {
		return nil
	}
	if err := writeState(s.path, updated); err != nil {
		return err
	}
	s.state = updated
	return nil
}

func (s *Store) ResetProtectedSubscriptions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.state == nil || len(s.state.Subscriptions) == 0 {
		return nil
	}
	updated := cloneState(s.state)
	updated.Version = stateVersion
	updated.Subscriptions = []Subscription{}
	if err := writeState(s.path, updated); err != nil {
		return err
	}
	s.state = updated
	return nil
}

func (s *Store) Remove(endpoint string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	if s.state == nil {
		return false, nil
	}
	updated := cloneState(s.state)
	for index, subscription := range updated.Subscriptions {
		if subscription.Endpoint != endpoint {
			continue
		}
		updated.Subscriptions = append(updated.Subscriptions[:index], updated.Subscriptions[index+1:]...)
		if err := writeState(s.path, updated); err != nil {
			return false, err
		}
		s.state = updated
		return true, nil
	}
	return false, nil
}

func Reset(path string) error {
	var resetErrors []error
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		resetErrors = append(resetErrors, fmt.Errorf("remove notification state: %w", err))
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			resetErrors = append(resetErrors, fmt.Errorf("inspect notification state directory: %w", err))
		}
		return errors.Join(resetErrors...)
	}
	for _, entry := range entries {
		if !recognizedStateTemporary(entry) {
			continue
		}
		if err := os.Remove(filepath.Join(filepath.Dir(path), entry.Name())); err != nil {
			resetErrors = append(resetErrors, fmt.Errorf("remove notification temporary state: %w", err))
		}
	}
	return errors.Join(resetErrors...)
}

func recognizedStateTemporary(entry os.DirEntry) bool {
	const prefix = ".notifications-"
	if !strings.HasPrefix(entry.Name(), prefix) {
		return false
	}
	suffix := strings.TrimPrefix(entry.Name(), prefix)
	if suffix == "" || len(suffix) > 10 {
		return false
	}
	for _, character := range suffix {
		if character < '0' || character > '9' {
			return false
		}
	}
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0
}

func readState(path string) (*persistedState, error) {
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("protect notification state directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("notification state is not a regular file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("protect notification state: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read notification state: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read notification state: %w", err)
	}
	if len(data) > maxStateBytes {
		return nil, errors.New("notification state is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state persistedState
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("decode notification state: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	if err := validateState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func validateState(state *persistedState) error {
	if state.Version != stateVersion && state.Version != ownedStateVersion && state.Version != legacyStateVersion {
		return fmt.Errorf("unsupported notification state version %d", state.Version)
	}
	contact, err := ValidateContact(state.VAPIDContact)
	if err != nil || contact != state.VAPIDContact {
		return errors.New("notification state has an invalid VAPID contact")
	}
	privateKey, err := base64.RawURLEncoding.DecodeString(state.VAPIDPrivateKey)
	if err != nil || len(privateKey) != 32 {
		return errors.New("notification state has an invalid VAPID private key")
	}
	privateScalar := new(big.Int).SetBytes(privateKey)
	if privateScalar.Sign() == 0 || privateScalar.Cmp(elliptic.P256().Params().N) >= 0 {
		return errors.New("notification state has an invalid VAPID private key")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(state.VAPIDPublicKey)
	if err != nil || len(publicKey) != 65 || publicKey[0] != 4 {
		return errors.New("notification state has an invalid VAPID public key")
	}
	x, y := elliptic.P256().ScalarBaseMult(privateKey)
	if !bytes.Equal(publicKey, elliptic.Marshal(elliptic.P256(), x, y)) {
		return errors.New("notification state VAPID keys do not match")
	}
	if len(state.Subscriptions) > MaxSubscriptions {
		return errors.New("notification state has too many subscriptions")
	}
	seen := make(map[string]struct{}, len(state.Subscriptions))
	for _, subscription := range state.Subscriptions {
		if _, exists := seen[subscription.Endpoint]; exists {
			return errors.New("notification state has duplicate subscriptions")
		}
		seen[subscription.Endpoint] = struct{}{}
		if err := validateSubscriptionShape(subscription); err != nil {
			return fmt.Errorf("notification state has an invalid subscription: %w", err)
		}
		if state.Version >= ownedStateVersion && subscription.TrustID != "" {
			if decoded, err := base64.RawURLEncoding.DecodeString(subscription.TrustID); err != nil || len(decoded) != 16 {
				return errors.New("notification state has an invalid subscription owner")
			}
		}
	}
	return nil
}

func validateSubscriptionShape(subscription Subscription) error {
	if _, err := validateEndpointURL(subscription.Endpoint); err != nil {
		return err
	}
	if subscription.ExpirationTime != nil && (*subscription.ExpirationTime < 0 || math.IsInf(*subscription.ExpirationTime, 0) || math.IsNaN(*subscription.ExpirationTime)) {
		return errors.New("expiration time must be a non-negative finite number")
	}
	auth, err := decodeURLBase64(subscription.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("invalid authentication key")
	}
	p256dh, err := decodeURLBase64(subscription.Keys.P256dh)
	if err != nil || len(p256dh) != 65 || p256dh[0] != 4 {
		return errors.New("invalid p256dh key")
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), p256dh)
	if x == nil || y == nil {
		return errors.New("p256dh key is not on P-256")
	}
	return nil
}

func decodeURLBase64(value string) ([]byte, error) {
	if len(value) == 0 || len(value) > 256 {
		return nil, errors.New("invalid base64url value")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func writeState(path string, state *persistedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode notification state: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create notification state directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect notification state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".notifications-*")
	if err != nil {
		return fmt.Errorf("create notification state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect notification state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write notification state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync notification state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close notification state: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace notification state: %w", err)
	}
	directoryFile, err := os.Open(directory)
	if err == nil {
		_ = directoryFile.Sync()
		_ = directoryFile.Close()
	}
	return nil
}

func cloneState(state *persistedState) *persistedState {
	copy := *state
	copy.Subscriptions = append([]Subscription(nil), state.Subscriptions...)
	return &copy
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("notification state contains extra JSON")
		}
		return fmt.Errorf("decode notification state: %w", err)
	}
	return nil
}
