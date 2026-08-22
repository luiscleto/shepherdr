package access

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Store struct {
	path  string
	lock  *os.File
	state *State
}

type OpenOptions struct {
	Path               string
	PublicOrigin       string
	SessionLifetime    string
	SessionLifetimeSet bool
	Now                time.Time
}

type OpenResult struct {
	BootstrapToken string
	Origin         Origin
	Store          *Store
}

func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(directory, "shepherdr", "access.json"), nil
}

func OpenProtected(options OpenOptions) (OpenResult, error) {
	if options.Path == "" {
		return OpenResult{}, errors.New("access state path is required")
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	var suppliedOrigin Origin
	var err error
	if options.PublicOrigin != "" {
		if suppliedOrigin, err = ParseCanonicalOrigin(options.PublicOrigin); err != nil {
			return OpenResult{}, err
		}
	}
	lifetimeText := options.SessionLifetime
	if !options.SessionLifetimeSet {
		lifetimeText = DefaultSessionLifetime
	}
	lifetime, err := ParseSessionLifetime(lifetimeText)
	if err != nil {
		return OpenResult{}, err
	}
	if err := ensurePrivateDirectory(filepath.Dir(options.Path), true); err != nil {
		return OpenResult{}, err
	}
	lock, err := acquireLock(filepath.Join(filepath.Dir(options.Path), "access.lock"), true)
	if err != nil {
		return OpenResult{}, err
	}
	store := &Store{path: options.Path, lock: lock}
	closeOnError := func(err error) (OpenResult, error) {
		store.Close()
		return OpenResult{}, err
	}
	state, readErr := readState(options.Path)
	if errors.Is(readErr, os.ErrNotExist) {
		if options.PublicOrigin == "" {
			return closeOnError(errors.New("protected access is not initialized; start with -public-origin https://your-private-shepherdr-host"))
		}
		handle, err := randomBytes(64)
		if err != nil {
			return closeOnError(err)
		}
		state = &State{
			Version: stateVersion, PublicOrigin: suppliedOrigin.Value, RPID: suppliedOrigin.RPID,
			SessionLifetime: lifetime.String(), UserHandle: base64.RawURLEncoding.EncodeToString(handle),
			Credentials: []CredentialRecord{}, Sessions: []SessionRecord{}, Invitations: []InvitationRecord{},
		}
	} else if readErr != nil {
		return closeOnError(readErr)
	} else {
		persistedOrigin, err := ParseCanonicalOrigin(state.PublicOrigin)
		if err != nil {
			return closeOnError(err)
		}
		if options.PublicOrigin != "" && suppliedOrigin != persistedOrigin {
			return closeOnError(errors.New("-public-origin does not match the initialized protected origin; changing an origin is not supported"))
		}
		suppliedOrigin = persistedOrigin
		if options.SessionLifetimeSet {
			state.SessionLifetime = lifetime.String()
		}
	}
	store.state = state
	changed := readErr != nil || options.SessionLifetimeSet
	invitationCount := len(state.Invitations)
	pruneInvitations(state, options.Now)
	changed = changed || invitationCount != len(state.Invitations)
	var bootstrapToken string
	if activeCredentialCount(state) == 0 && len(state.Invitations) == 0 {
		var invitation InvitationRecord
		bootstrapToken, invitation, err = newInvitation(options.Now, "")
		if err != nil {
			return closeOnError(err)
		}
		state.Invitations = append(state.Invitations, invitation)
		changed = true
	}
	if changed {
		if err := store.write(); err != nil {
			return closeOnError(err)
		}
	}
	return OpenResult{BootstrapToken: bootstrapToken, Origin: suppliedOrigin, Store: store}, nil
}

func OpenExistingStopped(path string) (*Store, Origin, error) {
	if err := ensurePrivateDirectory(filepath.Dir(path), false); err != nil {
		return nil, Origin{}, err
	}
	lock, err := acquireLock(filepath.Join(filepath.Dir(path), "access.lock"), false)
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, Origin{}, errors.New("Stop Shepherdr first")
		}
		return nil, Origin{}, err
	}
	store := &Store{path: path, lock: lock}
	state, err := readState(path)
	if err != nil {
		store.Close()
		return nil, Origin{}, err
	}
	store.state = state
	origin, err := ParseCanonicalOrigin(state.PublicOrigin)
	if err != nil {
		store.Close()
		return nil, Origin{}, err
	}
	return store, origin, nil
}

func HoldServiceLock(path string) (*os.File, error) {
	if path == "" {
		return nil, errors.New("access state path is required")
	}
	if err := ensurePrivateDirectory(filepath.Dir(path), true); err != nil {
		return nil, err
	}
	lock, err := acquireLock(filepath.Join(filepath.Dir(path), "access.lock"), true)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func (s *Store) Close() {
	if s == nil || s.lock == nil {
		return
	}
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	_ = s.lock.Close()
	s.lock = nil
}

func (s *Store) write() error {
	if err := validateState(s.state); err != nil {
		return err
	}
	return writeState(s.path, s.state)
}

func readState(path string) (*State, error) {
	file, err := openPrivateRegular(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read access state: %w", err)
	}
	if len(data) > maxStateBytes {
		return nil, errors.New("access state is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("decode access state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("access state contains extra JSON")
	}
	if err := validateState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeState(path string, state *State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode access state: %w", err)
	}
	if len(data)+1 > maxStateBytes {
		return errors.New("access state is too large")
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory, true); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		file, err := openPrivateRegular(path)
		if err != nil {
			return err
		}
		_ = file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".access-*")
	if err != nil {
		return fmt.Errorf("create access state: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write access state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync access state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace access state: %w", err)
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open access state directory: %w", err)
	}
	defer directoryFile.Close()
	if err := directoryFile.Sync(); err != nil {
		return fmt.Errorf("sync access state directory: %w", err)
	}
	return nil
}

func ensurePrivateDirectory(directory string, create bool) error {
	if create {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create access state directory: %w", err)
		}
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect access state directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return errors.New("access state directory must be an owner-only 0700 directory owned by the current user")
	}
	return nil
}

func openPrivateRegular(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || linkCount(info) != 1 {
		file.Close()
		return nil, errors.New("access state must be one owner-only 0600 regular file owned by the current user")
	}
	return file, nil
}

func acquireLock(path string, create bool) (*os.File, error) {
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if create {
		flags |= syscall.O_CREAT
	}
	fd, err := syscall.Open(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || linkCount(info) != 1 {
		file.Close()
		return nil, errors.New("access lock must be one owner-only 0600 regular file owned by the current user")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func linkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, fmt.Errorf("read secure random data: %w", err)
	}
	return value, nil
}

func randomToken(size int) (string, error) {
	value, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func activeCredentialCount(state *State) int {
	count := 0
	for _, record := range state.Credentials {
		if record.RevokedAt == nil {
			count++
		}
	}
	return count
}

func newInvitation(now time.Time, issuer string) (string, InvitationRecord, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", InvitationRecord{}, err
	}
	now = now.UTC()
	return token, InvitationRecord{
		Digest: digestToken(token), IssuerTrustID: issuer, IssuedAt: now, ExpiresAt: now.Add(InvitationLifetime),
	}, nil
}
