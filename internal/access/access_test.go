package access

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

type testNotificationAuthority struct {
	added   []string
	removed []string
	resets  int
}

func (a *testNotificationAuthority) TrustedSignInAdded(label string) {
	a.added = append(a.added, label)
}

func (a *testNotificationAuthority) RemoveTrustSubscriptions(trustID, _ string) error {
	a.removed = append(a.removed, trustID)
	return nil
}

func (a *testNotificationAuthority) ResetProtectedSubscriptions() error {
	a.resets++
	return nil
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func TestCanonicalOriginAndSessionLifetimeBoundaries(t *testing.T) {
	valid := map[string]Origin{
		"https://shepherdr":              {Authority: "shepherdr", RPID: "shepherdr", Value: "https://shepherdr"},
		"https://shepherdr.private":      {Authority: "shepherdr.private", RPID: "shepherdr.private", Value: "https://shepherdr.private"},
		"https://shepherdr.private:8443": {Authority: "shepherdr.private:8443", RPID: "shepherdr.private", Value: "https://shepherdr.private:8443"},
	}
	for value, want := range valid {
		if got, err := ParseCanonicalOrigin(value); err != nil || got != want {
			t.Errorf("ParseCanonicalOrigin(%q) = %+v, %v; want %+v", value, got, err, want)
		}
	}
	for _, value := range []string{
		"http://shepherdr.private", "https://Shepherdr.private", "https://127.0.0.1", "https://localhost",
		"https://shepherdr.localhost", "https://shepherdr.private/", "https://shepherdr.private:443",
		"https://user@shepherdr.private", "https://shepherdr.private?x=1", " https://shepherdr.private",
		"https://2130706433", "https://0177.0.0.1", "https://0x7f000001", "https://shepherdr.private:08443",
	} {
		if _, err := ParseCanonicalOrigin(value); err == nil {
			t.Errorf("ParseCanonicalOrigin(%q) unexpectedly succeeded", value)
		}
	}
	for _, value := range []string{"1d", "30d", "365d", "none"} {
		if _, err := ParseSessionLifetime(value); err != nil {
			t.Errorf("ParseSessionLifetime(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "0d", "366d", "1000d", "+1d", "1h", "01d"} {
		if _, err := ParseSessionLifetime(value); err == nil {
			t.Errorf("ParseSessionLifetime(%q) unexpectedly succeeded", value)
		}
	}
}

func TestProtectedStoreIsVersionedPrivateLockedAndOriginStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "access.json")
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	first, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if first.BootstrapToken == "" || first.Store.state.Version != stateVersion || first.Store.state.SessionLifetime != DefaultSessionLifetime {
		t.Fatalf("first open state=%+v bootstrap present=%t", first.Store.state, first.BootstrapToken != "")
	}
	for target, want := range map[string]os.FileMode{filepath.Dir(path): 0o700, path: 0o600, filepath.Join(filepath.Dir(path), "access.lock"): 0o600} {
		info, statErr := os.Stat(target)
		if statErr != nil || info.Mode().Perm() != want {
			t.Errorf("%s mode=%v err=%v, want %o", target, info.Mode().Perm(), statErr, want)
		}
	}
	if _, _, err := OpenExistingStopped(path); err == nil || err.Error() != "Stop Shepherdr first" {
		t.Fatalf("stopped-service lock check = %v", err)
	}
	first.Store.Close()

	second, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private", Now: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if second.BootstrapToken != "" {
		t.Fatal("restart exposed the stored invitation bearer token")
	}
	second.Store.Close()
	if _, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://other.private", Now: now}); err == nil {
		t.Fatal("initialized origin changed")
	}

	hardlink := path + ".link"
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenExistingStopped(path); err == nil {
		fatalState(t, "multiply linked state was accepted")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenExistingStopped(path); err == nil {
		fatalState(t, "unsafe state permissions were accepted")
	}
}

func TestServiceLockExcludesStartupAndStoppedCommandsWithoutChangingAccessState(t *testing.T) {
	t.Run("uninitialized sign-in-off", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state", "access.json")
		lock, err := HoldServiceLock(path)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("sign-in-off changed absent access state: %v", err)
		}
		lockInfo, err := lock.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if lockInfo.Mode().Perm() != 0o600 {
			t.Fatalf("service lock mode=%v", lockInfo.Mode().Perm())
		}
		if _, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private"}); err == nil {
			t.Fatal("first protected start raced an active sign-in-off service")
		}
		if _, _, err := OpenExistingStopped(path); err == nil || err.Error() != "Stop Shepherdr first" {
			t.Fatalf("stopped access command while sign-in-off is live = %v", err)
		}
		if _, err := HoldServiceLock(path); err == nil {
			t.Fatal("stopped reset lock raced an active sign-in-off service")
		}
	})

	t.Run("existing bytes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state", "access.json")
		opened, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private"})
		if err != nil {
			t.Fatal(err)
		}
		opened.Store.Close()
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := HoldServiceLock(path)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		lock.Close()
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("sign-in-off changed initialized access state: equal=%t err=%v", bytes.Equal(before, after), err)
		}
	})
}

func TestFirstProtectedStartAndServiceLockRaceHasOneWinner(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		path := filepath.Join(t.TempDir(), "state", "access.json")
		start := make(chan struct{})
		release := make(chan struct{})
		type result struct {
			kind string
			err  error
		}
		results := make(chan result, 2)
		go func() {
			<-start
			opened, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private"})
			if err != nil {
				results <- result{kind: "protected", err: err}
				return
			}
			results <- result{kind: "protected"}
			<-release
			opened.Store.Close()
		}()
		go func() {
			<-start
			lock, err := HoldServiceLock(path)
			if err != nil {
				results <- result{kind: "sign-in-off", err: err}
				return
			}
			results <- result{kind: "sign-in-off"}
			<-release
			_ = lock.Close()
		}()
		close(start)
		first := <-results
		second := <-results
		winners := 0
		for _, candidate := range []result{first, second} {
			if candidate.err == nil {
				winners++
			}
		}
		close(release)
		if winners != 1 {
			t.Fatalf("race %d winners=%d: %s=%v %s=%v", attempt, winners, first.kind, first.err, second.kind, second.err)
		}
	}
}

func TestProtectedStoreRejectsMissingCorruptUnsupportedUnknownAndSymlinkedState(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	if _, err := OpenProtected(OpenOptions{Path: filepath.Join(t.TempDir(), "missing", "access.json"), Now: now}); err == nil {
		t.Fatal("missing protected state started without a public origin")
	}
	for _, test := range []struct {
		name   string
		change func(t *testing.T, path string, state *State)
	}{
		{"corrupt", func(t *testing.T, path string, _ *State) { writeTestStateBytes(t, path, []byte("{not-json")) }},
		{"unsupported", func(t *testing.T, path string, state *State) {
			state.Version = 99
			writeTestStateJSON(t, path, state)
		}},
		{"unknown field", func(t *testing.T, path string, state *State) {
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data[:len(data)-1], []byte(`,"security_bypass":true}`)...)
			writeTestStateBytes(t, path, data)
		}},
		{"oversized", func(t *testing.T, path string, _ *State) {
			writeTestStateBytes(t, path, bytes.Repeat([]byte("x"), maxStateBytes+1))
		}},
		{"symlink", func(t *testing.T, path string, state *State) {
			target := path + ".target"
			writeTestStateJSON(t, target, state)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state", "access.json")
			opened, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private", Now: now})
			if err != nil {
				t.Fatal(err)
			}
			state, err := cloneState(opened.Store.state)
			opened.Store.Close()
			if err != nil {
				t.Fatal(err)
			}
			test.change(t, path, state)
			if _, err := OpenProtected(OpenOptions{Path: path, Now: now}); err == nil {
				t.Fatal("unsafe state was accepted")
			}
		})
	}
}

func TestInvitationReservationIsBoundedAndPasskeyPolicyIsExact(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	result, err := OpenProtected(OpenOptions{Path: filepath.Join(t.TempDir(), "state", "access.json"), PublicOrigin: "https://shepherdr.private", Now: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(result.Store, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	defer result.Store.Close()
	clientA := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	clientB := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	if _, err := manager.BeginTrust(clientA, result.BootstrapToken, "   "); !errors.Is(err, ErrInvitationGeneric) {
		t.Fatalf("blank trusted sign-in label result = %v", err)
	}
	begin, err := manager.BeginTrust(clientA, result.BootstrapToken, "Phone <script>")
	if err != nil {
		t.Fatal(err)
	}
	creation, ok := begin.Options.(*protocol.CredentialCreation)
	if !ok {
		t.Fatalf("registration options type = %T", begin.Options)
	}
	selection := creation.Response.AuthenticatorSelection
	if selection.RequireResidentKey == nil || !*selection.RequireResidentKey || selection.ResidentKey != protocol.ResidentKeyRequirementRequired ||
		selection.UserVerification != protocol.VerificationRequired || selection.AuthenticatorAttachment != "" || creation.Response.Attestation != protocol.PreferNoAttestation {
		t.Fatalf("unexpected passkey policy: %+v attestation=%q", selection, creation.Response.Attestation)
	}
	if begin.TTL != ReservationLifetime || !begin.ExpiresAt.Equal(clock.Now().Add(ReservationLifetime)) {
		t.Fatalf("reservation ttl=%v expiry=%v", begin.TTL, begin.ExpiresAt)
	}
	if _, err := manager.BeginTrust(clientB, result.BootstrapToken, "Other"); !errors.Is(err, ErrInvitationGeneric) {
		t.Fatalf("racing client result = %v", err)
	}
	clock.Advance(4 * time.Minute)
	retry, err := manager.BeginTrust(clientA, result.BootstrapToken, "Phone <script>")
	if err != nil || !retry.ExpiresAt.Equal(begin.ExpiresAt) {
		t.Fatalf("same-client retry expiry=%v err=%v, want original %v", retry.ExpiresAt, err, begin.ExpiresAt)
	}
	clock.Advance(time.Minute)
	if _, err := manager.BeginTrust(clientB, result.BootstrapToken, "Other"); err != nil {
		t.Fatalf("abandoned reservation did not release: %v", err)
	}
	clock.Advance(ReservationLifetime)
	if _, err := manager.BeginTrust(clientA, result.BootstrapToken, "Expired"); !errors.Is(err, ErrInvitationGeneric) {
		t.Fatalf("expired invitation result = %v", err)
	}
	if _, err := manager.BeginTrust(clientA, "malformed", "Malformed"); !errors.Is(err, ErrInvitationGeneric) {
		t.Fatalf("malformed invitation result = %v", err)
	}
}

func TestCeremonyRetriesKeepOneClientDeadlineAndAttemptBound(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, tokens, _ := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	identity, ok := manager.AuthenticateToken(tokens[0])
	if !ok {
		t.Fatal("seed session did not authenticate")
	}

	for _, test := range []struct {
		name   string
		client string
		begin  func(string) (CeremonyBegin, error)
		finish func(string, *http.Request) error
	}{
		{
			name: "sign-in", client: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{51}, 32)),
			begin: manager.BeginSignIn,
			finish: func(client string, request *http.Request) error {
				_, err := manager.FinishSignIn(client, request, nil)
				return err
			},
		},
		{
			name: "reauthentication", client: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{52}, 32)),
			begin: func(client string) (CeremonyBegin, error) { return manager.BeginReauthentication(client, identity) },
			finish: func(client string, request *http.Request) error {
				_, err := manager.FinishReauthentication(client, request, identity)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var deadline time.Time
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				begin, err := test.begin(test.client)
				if err != nil {
					t.Fatalf("begin attempt %d: %v", attempt, err)
				}
				if attempt == 1 {
					deadline = begin.ExpiresAt
				} else if !begin.ExpiresAt.Equal(deadline) {
					t.Fatalf("attempt %d extended client deadline to %v, want %v", attempt, begin.ExpiresAt, deadline)
				}
				err = test.finish(test.client, failedCeremonyRequest())
				if CeremonyCanRetry(err) != (attempt < maxAttempts) {
					t.Fatalf("finish attempt %d retryable=%t err=%v", attempt, CeremonyCanRetry(err), err)
				}
			}
			if _, err := test.begin(test.client); err == nil {
				t.Fatal("attempt limit accepted another challenge")
			}
		})
	}
}

func TestAssertionRetryChallengeEndsAtOriginalDeadline(t *testing.T) {
	for _, kind := range []string{CeremonySignIn, CeremonyReauth} {
		t.Run(kind, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
			manager, store, tokens, _ := seededManager(t, clock, 1, "30d")
			defer manager.Close()
			defer store.Close()
			identity, ok := manager.AuthenticateToken(tokens[0])
			if !ok {
				t.Fatal("seed session did not authenticate")
			}
			client := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(70 + len(kind))}, 32))
			begin := func() (CeremonyBegin, error) {
				if kind == CeremonyReauth {
					return manager.BeginReauthentication(client, identity)
				}
				return manager.BeginSignIn(client)
			}
			finish := func() error {
				if kind == CeremonyReauth {
					_, err := manager.FinishReauthentication(client, failedCeremonyRequest(), identity)
					return err
				}
				_, err := manager.FinishSignIn(client, failedCeremonyRequest(), nil)
				return err
			}
			first, err := begin()
			if err != nil {
				t.Fatal(err)
			}
			if err := finish(); !CeremonyCanRetry(err) {
				t.Fatalf("first failure was not retryable: %v", err)
			}
			clock.Advance(ReservationLifetime - time.Second)
			retry, err := begin()
			if err != nil || !retry.ExpiresAt.Equal(first.ExpiresAt) || retry.TTL != time.Second {
				t.Fatalf("deadline retry expiry=%v ttl=%v err=%v", retry.ExpiresAt, retry.TTL, err)
			}
			clock.Advance(time.Second)
			if err := finish(); CeremonyCanRetry(err) {
				t.Fatalf("deadline-expired challenge remained retryable: %v", err)
			}
		})
	}
}

func TestTrustRetryKeepsReservationAndDeadline(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, _ := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	link, _, err := manager.CreateLocalInvitation()
	if err != nil {
		t.Fatal(err)
	}
	invitationToken := strings.TrimPrefix(link, manager.origin.Value+"/#trust=")
	client := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{61}, 32))
	other := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{62}, 32))
	begin, err := manager.BeginTrust(client, invitationToken, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.FinishTrust(client, failedCeremonyRequest(), nil); !CeremonyCanRetry(err) {
		t.Fatalf("ordinary trust failure was not retryable: %v", err)
	}
	retry, err := manager.BeginTrust(client, invitationToken, "Phone")
	if err != nil || retry.ClientToken != client || !retry.ExpiresAt.Equal(begin.ExpiresAt) {
		t.Fatalf("trust retry token=%q expiry=%v err=%v", retry.ClientToken, retry.ExpiresAt, err)
	}
	if _, err := manager.BeginTrust(other, invitationToken, "Other"); !errors.Is(err, ErrInvitationGeneric) {
		t.Fatalf("retry released invitation reservation: %v", err)
	}
	clock.Advance(ReservationLifetime)
	if _, err := manager.FinishTrust(client, failedCeremonyRequest(), nil); CeremonyCanRetry(err) {
		t.Fatalf("expired trust challenge remained retryable: %v", err)
	}
}

func TestBeginCannotReplaceAnInFlightCeremony(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, _ := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	client := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{81}, 32))
	first, err := manager.BeginSignIn(client)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	manager.ceremonyHooks = &ceremonyVerificationHooks{assertion: func(*ceremony, *http.Request) (*webauthn.Credential, string, error) {
		close(started)
		<-release
		return nil, "", ErrUnauthorized
	}}
	finished := make(chan error, 1)
	go func() {
		_, err := manager.FinishSignIn(client, failedCeremonyRequest(), nil)
		finished <- err
	}()
	<-started
	key := ceremonyKey(CeremonySignIn, digestToken(client))
	manager.ceremonyMu.Lock()
	current := manager.ceremonies[key]
	attempts := manager.attempts[key]
	manager.ceremonyMu.Unlock()
	if current == nil || !current.inFlight || attempts.count != 1 {
		t.Fatalf("in-flight state ceremony=%+v attempts=%+v", current, attempts)
	}
	if _, err := manager.BeginSignIn(client); err == nil {
		t.Fatal("begin replaced a challenge whose verification was in flight")
	}
	manager.ceremonyMu.Lock()
	if manager.ceremonies[key] != current || manager.attempts[key].count != 1 {
		t.Fatal("refused begin changed the authoritative in-flight ceremony")
	}
	manager.ceremonyMu.Unlock()
	close(release)
	if err := <-finished; !CeremonyCanRetry(err) {
		t.Fatalf("ordinary in-flight failure was not retryable: %v", err)
	}
	manager.ceremonyMu.Lock()
	_, stillLive := manager.ceremonies[key]
	attempts = manager.attempts[key]
	manager.ceremonyMu.Unlock()
	if stillLive || attempts.count != 1 {
		t.Fatalf("resolved failure live=%t attempts=%+v", stillLive, attempts)
	}
	retry, err := manager.BeginSignIn(client)
	if err != nil || !retry.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("retry expiry=%v err=%v, want %v", retry.ExpiresAt, err, first.ExpiresAt)
	}
}

func TestTrustBeginCannotReplaceAnInFlightRegistration(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, _ := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	link, _, err := manager.CreateLocalInvitation()
	if err != nil {
		t.Fatal(err)
	}
	invitationToken := strings.TrimPrefix(link, manager.origin.Value+"/#trust=")
	client := ceremonyClient(82)
	first, err := manager.BeginTrust(client, invitationToken, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	manager.ceremonyHooks = &ceremonyVerificationHooks{registration: func(operatorUser, *ceremony, *http.Request) (*webauthn.Credential, error) {
		close(started)
		<-release
		return nil, ErrInvitationGeneric
	}}
	finished := make(chan error, 1)
	go func() {
		_, err := manager.FinishTrust(client, failedCeremonyRequest(), nil)
		finished <- err
	}()
	<-started
	_, racingBeginErr := manager.BeginTrust(client, invitationToken, "Phone")
	close(release)
	finishErr := <-finished
	if racingBeginErr == nil || !CeremonyCanRetry(finishErr) {
		t.Fatalf("racing trust begin=%v finish=%v", racingBeginErr, finishErr)
	}
	retry, err := manager.BeginTrust(client, invitationToken, "Phone")
	if err != nil || !retry.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("trust retry expiry=%v err=%v, want %v", retry.ExpiresAt, err, first.ExpiresAt)
	}
}

func TestConcurrentValidFinishesReserveOneAttemptAndIssueOneSession(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, trustIDs := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	client := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{82}, 32))
	if _, err := manager.BeginSignIn(client); err != nil {
		t.Fatal(err)
	}
	credential := manager.state.Credentials[0].Credential
	type finishEvent struct {
		kind  string
		issue SessionIssue
		err   error
	}
	events := make(chan finishEvent, 32)
	release := make(chan struct{})
	var verificationCalls atomic.Int32
	manager.ceremonyHooks = &ceremonyVerificationHooks{assertion: func(*ceremony, *http.Request) (*webauthn.Credential, string, error) {
		verificationCalls.Add(1)
		events <- finishEvent{kind: "verify"}
		<-release
		copy := credential
		return &copy, trustIDs[0], nil
	}}
	finish := func() {
		issue, err := manager.FinishSignIn(client, failedCeremonyRequest(), nil)
		events <- finishEvent{kind: "return", issue: issue, err: err}
	}
	go finish()
	if event := <-events; event.kind != "verify" {
		t.Fatalf("first finish event=%+v", event)
	}
	_, racingBeginErr := manager.BeginSignIn(client)
	key := ceremonyKey(CeremonySignIn, digestToken(client))
	manager.ceremonyMu.Lock()
	if current := manager.ceremonies[key]; current == nil || !current.inFlight || manager.attempts[key].count != 1 {
		manager.ceremonyMu.Unlock()
		t.Fatal("first finish did not atomically reserve one live attempt")
	}
	manager.ceremonyMu.Unlock()
	for index := 0; index < maxAttempts+2; index++ {
		go finish()
	}
	returned := 0
	for index := 0; index < maxAttempts+2; index++ {
		event := <-events
		if event.kind == "return" {
			returned++
		}
	}
	close(release)
	succeeded := 0
	for returned < maxAttempts+3 {
		event := <-events
		if event.kind != "return" {
			continue
		}
		returned++
		if event.err == nil && event.issue.Token != "" {
			succeeded++
		}
	}
	if racingBeginErr == nil || verificationCalls.Load() != 1 || succeeded != 1 {
		t.Fatalf("racing begin=%v verification calls=%d successful outcomes=%d", racingBeginErr, verificationCalls.Load(), succeeded)
	}
	manager.ceremonyMu.Lock()
	_, ceremonyLive := manager.ceremonies[key]
	_, attemptsLive := manager.attempts[key]
	manager.ceremonyMu.Unlock()
	if ceremonyLive || attemptsLive || len(manager.state.Sessions) != 2 {
		t.Fatalf("completion ceremony=%t attempts=%t sessions=%d", ceremonyLive, attemptsLive, len(manager.state.Sessions))
	}
}

func TestInFlightCeremonyCountsTowardCapacityAndCompletionReleasesIt(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, trustIDs := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	for index := 0; index < maxCeremonies-1; index++ {
		if _, err := manager.BeginSignIn(ceremonyClient(index)); err != nil {
			t.Fatalf("fill ceremony %d: %v", index, err)
		}
	}
	client := ceremonyClient(maxCeremonies - 1)
	if _, err := manager.BeginSignIn(client); err != nil {
		t.Fatal(err)
	}
	credential := manager.state.Credentials[0].Credential
	started := make(chan struct{})
	release := make(chan struct{})
	manager.ceremonyHooks = &ceremonyVerificationHooks{assertion: func(*ceremony, *http.Request) (*webauthn.Credential, string, error) {
		close(started)
		<-release
		copy := credential
		return &copy, trustIDs[0], nil
	}}
	finished := make(chan error, 1)
	go func() {
		_, err := manager.FinishSignIn(client, failedCeremonyRequest(), nil)
		finished <- err
	}()
	<-started
	manager.ceremonyMu.Lock()
	ceremonyCount, attemptCount := len(manager.ceremonies), len(manager.attempts)
	manager.ceremonyMu.Unlock()
	if ceremonyCount != maxCeremonies || attemptCount != maxCeremonies {
		t.Fatalf("capacity ceremonies=%d attempts=%d", ceremonyCount, attemptCount)
	}
	if _, err := manager.BeginSignIn(ceremonyClient(maxCeremonies)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("in-flight ceremony did not hold capacity: %v", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	manager.ceremonyMu.Lock()
	ceremonyCount, attemptCount = len(manager.ceremonies), len(manager.attempts)
	manager.ceremonyMu.Unlock()
	if ceremonyCount != maxCeremonies-1 || attemptCount != maxCeremonies-1 {
		t.Fatalf("completion leaked capacity ceremonies=%d attempts=%d", ceremonyCount, attemptCount)
	}
	if _, err := manager.BeginSignIn(ceremonyClient(maxCeremonies)); err != nil {
		t.Fatalf("released capacity could not be reused: %v", err)
	}
}

func TestCanceledAndTerminalInFlightCeremoniesResolveWithoutDetachedWork(t *testing.T) {
	for _, outcome := range []string{"canceled", "terminal"} {
		t.Run(outcome, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
			manager, store, _, trustIDs := seededManager(t, clock, 1, "30d")
			defer manager.Close()
			defer store.Close()
			client := ceremonyClient(220 + len(outcome))
			if _, err := manager.BeginSignIn(client); err != nil {
				t.Fatal(err)
			}
			manager.ceremonyHooks = &ceremonyVerificationHooks{assertion: func(*ceremony, *http.Request) (*webauthn.Credential, string, error) {
				if outcome == "canceled" {
					return nil, "", context.Canceled
				}
				return &webauthn.Credential{ID: []byte{255}, PublicKey: []byte{254}}, trustIDs[0], nil
			}}
			_, err := manager.FinishSignIn(client, failedCeremonyRequest(), nil)
			if outcome == "canceled" && !CeremonyCanRetry(err) {
				t.Fatalf("canceled verification was not retryable: %v", err)
			}
			if outcome == "terminal" && CeremonyCanRetry(err) {
				t.Fatalf("unrecoverable commit was retryable: %v", err)
			}
			key := ceremonyKey(CeremonySignIn, digestToken(client))
			manager.ceremonyMu.Lock()
			_, ceremonyLive := manager.ceremonies[key]
			attempts, attemptsLive := manager.attempts[key]
			manager.ceremonyMu.Unlock()
			if ceremonyLive || outcome == "terminal" && attemptsLive || outcome == "canceled" && (!attemptsLive || attempts.count != 1) {
				t.Fatalf("outcome=%s ceremony=%t attempts=%+v live=%t", outcome, ceremonyLive, attempts, attemptsLive)
			}
			if _, err := manager.BeginSignIn(client); err != nil {
				t.Fatalf("outcome=%s left client unable to begin: %v", outcome, err)
			}
		})
	}
}

func TestExpiredInFlightCeremonyCannotDetachOrCommit(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, trustIDs := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	client := ceremonyClient(240)
	if _, err := manager.BeginSignIn(client); err != nil {
		t.Fatal(err)
	}
	credential := manager.state.Credentials[0].Credential
	started := make(chan struct{})
	release := make(chan struct{})
	manager.ceremonyHooks = &ceremonyVerificationHooks{assertion: func(*ceremony, *http.Request) (*webauthn.Credential, string, error) {
		close(started)
		<-release
		copy := credential
		return &copy, trustIDs[0], nil
	}}
	finished := make(chan error, 1)
	go func() {
		_, err := manager.FinishSignIn(client, failedCeremonyRequest(), nil)
		finished <- err
	}()
	<-started
	clock.Advance(ReservationLifetime)
	_, beginErr := manager.BeginSignIn(client)
	key := ceremonyKey(CeremonySignIn, digestToken(client))
	manager.ceremonyMu.Lock()
	current := manager.ceremonies[key]
	_, attemptsLive := manager.attempts[key]
	manager.ceremonyMu.Unlock()
	accounted := current != nil && current.inFlight && attemptsLive
	close(release)
	finishErr := <-finished
	if beginErr == nil || !accounted || finishErr == nil || CeremonyCanRetry(finishErr) {
		t.Fatalf("expired begin=%v accounted=%t finish=%v", beginErr, accounted, finishErr)
	}
	manager.ceremonyMu.Lock()
	_, ceremonyLive := manager.ceremonies[key]
	_, attemptsLive = manager.attempts[key]
	manager.ceremonyMu.Unlock()
	if ceremonyLive || attemptsLive || len(manager.state.Sessions) != 1 {
		t.Fatalf("expired resolution ceremony=%t attempts=%t sessions=%d", ceremonyLive, attemptsLive, len(manager.state.Sessions))
	}
}

func TestAssertionCommitRechecksCeremonyDeadlineInsideAuthorityGate(t *testing.T) {
	for _, test := range []struct {
		name string
		kind string
	}{
		{name: "sign-in", kind: CeremonySignIn},
		{name: "reauthentication", kind: CeremonyReauth},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
			manager, store, tokens, trustIDs := seededManager(t, clock, 1, "30d")
			defer manager.Close()
			defer store.Close()
			identity, ok := manager.AuthenticateToken(tokens[0])
			if !ok {
				t.Fatal("seed session did not authenticate")
			}
			lease, ok := manager.Bind(identity)
			if !ok {
				t.Fatal("seed session did not bind")
			}
			defer lease.Close()

			client := ceremonyClient(245 + len(test.name))
			var begin CeremonyBegin
			var err error
			if test.kind == CeremonyReauth {
				begin, err = manager.BeginReauthentication(client, identity)
			} else {
				begin, err = manager.BeginSignIn(client)
			}
			if err != nil {
				t.Fatal(err)
			}
			beforeState, err := json.Marshal(manager.state)
			if err != nil {
				t.Fatal(err)
			}
			beforeFile, err := os.ReadFile(store.path)
			if err != nil {
				t.Fatal(err)
			}

			credential := manager.state.Credentials[0].Credential
			verified := make(chan struct{})
			waitingForGate := make(chan struct{})
			manager.ceremonyHooks = &ceremonyVerificationHooks{
				assertion: func(*ceremony, *http.Request) (*webauthn.Credential, string, error) {
					if !clock.Now().Before(begin.ExpiresAt) {
						return nil, "", errors.New("verification reached the ceremony deadline")
					}
					close(verified)
					copy := credential
					return &copy, trustIDs[0], nil
				},
				beforeAssertionCommit: func() {
					close(waitingForGate)
				},
			}

			type finishResult struct {
				issue SessionIssue
				err   error
			}
			finished := make(chan finishResult, 1)
			manager.gate.Lock()
			go func() {
				var issue SessionIssue
				var err error
				if test.kind == CeremonyReauth {
					issue, err = manager.FinishReauthentication(client, failedCeremonyRequest(), identity)
				} else {
					issue, err = manager.FinishSignIn(client, failedCeremonyRequest(), nil)
				}
				finished <- finishResult{issue: issue, err: err}
			}()
			<-verified
			<-waitingForGate
			clock.Advance(begin.ExpiresAt.Sub(clock.Now()))
			manager.gate.Unlock()

			result := <-finished
			if !errors.Is(result.err, ErrUnauthorized) || CeremonyCanRetry(result.err) {
				t.Fatalf("deadline commit error=%v retryable=%t", result.err, CeremonyCanRetry(result.err))
			}
			if result.issue.Token != "" || result.issue.ExpiresAt != nil || len(result.issue.Runtimes) != 0 {
				t.Fatalf("deadline commit returned an issue: %+v", result.issue)
			}
			afterState, err := json.Marshal(manager.state)
			if err != nil {
				t.Fatal(err)
			}
			afterFile, err := os.ReadFile(store.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterState, beforeState) || !bytes.Equal(afterFile, beforeFile) {
				t.Fatal("deadline commit mutated access state")
			}
			if len(manager.state.Sessions) != 1 || !manager.Recheck(identity) || !lease.Valid() {
				t.Fatalf("deadline commit changed the existing session: sessions=%d recheck=%t lease=%t", len(manager.state.Sessions), manager.Recheck(identity), lease.Valid())
			}
			if _, ok := manager.AuthenticateToken(tokens[0]); !ok {
				t.Fatal("deadline commit invalidated the existing session token")
			}
			key := ceremonyKey(test.kind, digestToken(client))
			manager.ceremonyMu.Lock()
			_, ceremonyLive := manager.ceremonies[key]
			_, attemptsLive := manager.attempts[key]
			manager.ceremonyMu.Unlock()
			if ceremonyLive || attemptsLive {
				t.Fatalf("terminal deadline leaked ceremony=%t attempts=%t", ceremonyLive, attemptsLive)
			}
		})
	}
}

func ceremonyClient(index int) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(index + 1)}, 32))
}

func failedCeremonyRequest() *http.Request {
	request := httptest.NewRequest(http.MethodPost, "https://shepherdr.private/finish", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestExpiredBootstrapIsPrunedAndReplacedOnlyOnALaterProtectedStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "access.json")
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	first, err := OpenProtected(OpenOptions{Path: path, PublicOrigin: "https://shepherdr.private", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	firstToken := first.BootstrapToken
	first.Store.Close()
	restarted, err := OpenProtected(OpenOptions{Path: path, Now: now.Add(InvitationLifetime)})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Store.Close()
	if restarted.BootstrapToken == "" || restarted.BootstrapToken == firstToken || len(restarted.Store.state.Invitations) != 1 {
		t.Fatalf("replacement bootstrap present=%t changed=%t invitations=%d", restarted.BootstrapToken != "", restarted.BootstrapToken != firstToken, len(restarted.Store.state.Invitations))
	}
}

func TestSessionExpiryRotationCapacityAndCleanup(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, tokens, trustIDs := seededManager(t, clock, 1, "365d")
	defer manager.Close()
	defer store.Close()
	identity, ok := manager.AuthenticateToken(tokens[0])
	if !ok {
		t.Fatal("seed session did not authenticate")
	}
	lease, ok := manager.Bind(identity)
	if !ok {
		t.Fatal("seed session did not bind")
	}
	clock.Advance(365 * 24 * time.Hour)
	runtimes, err := manager.expireNow()
	if err != nil || len(runtimes) != 1 || manager.Recheck(identity) {
		t.Fatalf("expiry runtimes=%d recheck=%t err=%v", len(runtimes), manager.Recheck(identity), err)
	}
	select {
	case <-lease.Context().Done():
	default:
		t.Fatal("expired session did not cancel its bound context")
	}
	waited := make(chan struct{})
	go func() { WaitRuntimes(runtimes); close(waited) }()
	select {
	case <-waited:
		t.Fatal("invalidation reported cleanup before the bound user released")
	case <-time.After(25 * time.Millisecond):
	}
	lease.Close()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("invalidation did not collect the bound user")
	}

	state, err := cloneState(manager.state)
	if err != nil {
		t.Fatal(err)
	}
	state.SessionLifetime = "none"
	for index := 0; index <= MaxTrustSessions; index++ {
		issue, invalidated, addErr := manager.addSession(state, trustIDs[0], trustIDs[0], clock.Now().Add(time.Duration(index)*time.Second), "")
		if addErr != nil || issue.ExpiresAt != nil {
			t.Fatalf("none session %d issue=%+v err=%v", index, issue, addErr)
		}
		if index == MaxTrustSessions && len(invalidated) != 1 {
			t.Fatalf("capacity invalidated %d sessions, want 1", len(invalidated))
		}
	}
	if got := trustSessionCount(state, trustIDs[0]); got != MaxTrustSessions {
		t.Fatalf("per-passkey sessions=%d, want %d", got, MaxTrustSessions)
	}
}

func TestSessionCapacityEvictsOnlyTargetWhenBothLimitsAreReached(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, trustIDs := seededManager(t, clock, 8, "none")
	defer manager.Close()
	defer store.Close()
	state, err := cloneState(manager.state)
	if err != nil {
		t.Fatal(err)
	}
	state.Sessions = nil
	globalOldest := digestToken("unrelated-global-oldest")
	for index := 0; index < MaxSessions-MaxTrustSessions; index++ {
		digest := digestToken("unrelated-" + fmt.Sprint(index))
		if index == 0 {
			digest = globalOldest
		}
		createdAt := clock.Now().Add(time.Duration(index) * time.Second)
		trustID := trustIDs[1+index/MaxTrustSessions]
		state.Sessions = append(state.Sessions, SessionRecord{
			Digest: digest, TrustID: trustID, CreatedAt: createdAt, Lifetime: "none",
			FreshTrustID: trustID, FreshAt: createdAt,
		})
	}
	targetOldest := digestToken("target-oldest")
	for index := 0; index < MaxTrustSessions; index++ {
		digest := digestToken("target-" + fmt.Sprint(index))
		if index == 0 {
			digest = targetOldest
		}
		createdAt := clock.Now().Add(time.Hour + time.Duration(index)*time.Second)
		state.Sessions = append(state.Sessions, SessionRecord{
			Digest: digest, TrustID: trustIDs[0], CreatedAt: createdAt, Lifetime: "none",
			FreshTrustID: trustIDs[0], FreshAt: createdAt,
		})
	}
	_, invalidated, err := manager.addSession(state, trustIDs[0], trustIDs[0], clock.Now().Add(2*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidated) != 1 || invalidated[0] != targetOldest {
		t.Fatalf("simultaneous caps invalidated %v, want only %q", invalidated, targetOldest)
	}
	if len(state.Sessions) != MaxSessions || trustSessionCount(state, trustIDs[0]) != MaxTrustSessions || oldestSessionIndex(state, "") < 0 {
		t.Fatalf("post-cap sessions=%d target=%d", len(state.Sessions), trustSessionCount(state, trustIDs[0]))
	}
	if !slices.ContainsFunc(state.Sessions, func(record SessionRecord) bool { return record.Digest == globalOldest }) {
		t.Fatal("unrelated global-oldest session was evicted")
	}
	if err := validateState(state); err != nil {
		t.Fatalf("minimal eviction left invalid state: %v", err)
	}
}

func TestFiniteAndBrowserSessionsPersistAcrossRestartWithoutSliding(t *testing.T) {
	for _, lifetime := range []string{"30d", "none"} {
		t.Run(lifetime, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
			manager, store, tokens, _ := seededManager(t, clock, 1, lifetime)
			path := store.path
			created := manager.state.Sessions[0].CreatedAt
			var expires *time.Time
			if manager.state.Sessions[0].ExpiresAt != nil {
				copy := *manager.state.Sessions[0].ExpiresAt
				expires = &copy
			}
			if _, ok := manager.AuthenticateToken(tokens[0]); !ok {
				t.Fatal("session did not authenticate before restart")
			}
			manager.Close()
			store.Close()
			clock.Advance(24 * time.Hour)
			reopened, _, err := OpenExistingStopped(path)
			if err != nil {
				t.Fatal(err)
			}
			restarted, err := NewManager(reopened, clock)
			if err != nil {
				reopened.Close()
				t.Fatal(err)
			}
			defer restarted.Close()
			defer reopened.Close()
			if _, ok := restarted.AuthenticateToken(tokens[0]); !ok {
				t.Fatal("session did not survive restart")
			}
			record := restarted.state.Sessions[0]
			if !record.CreatedAt.Equal(created) || expires == nil != (record.ExpiresAt == nil) || expires != nil && !record.ExpiresAt.Equal(*expires) {
				t.Fatalf("session slid across restart: before created=%v expires=%v after=%+v", created, expires, record)
			}
		})
	}
}

func TestRegistrationConsumesInvitationAndRotatesAnExistingSessionAtomically(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, tokens, _ := seededManager(t, clock, 1, "30d")
	defer manager.Close()
	defer store.Close()
	authority := &testNotificationAuthority{}
	manager.SetNotificationAuthority(authority)
	invitationToken, invitation, err := newInvitation(clock.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	clientToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	clientDigest := digestToken(clientToken)
	reservedUntil := clock.Now().Add(ReservationLifetime)
	invitation.ReservedClientDigest = clientDigest
	invitation.ReservedUntil = &reservedUntil
	manager.gate.Lock()
	updated, err := cloneState(manager.state)
	if err == nil {
		updated.Invitations = append(updated.Invitations, invitation)
		err = manager.commitLocked(updated)
	}
	manager.gate.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	old, ok := manager.AuthenticateToken(tokens[0])
	if !ok {
		t.Fatal("old session was unavailable")
	}
	current := &ceremony{invitationDigest: digestToken(invitationToken), clientDigest: clientDigest, label: "New passkey"}
	type registrationResult struct {
		issue SessionIssue
		err   error
	}
	results := make(chan registrationResult, 2)
	for index := 0; index < 2; index++ {
		go func(id byte) {
			issue, err := manager.commitRegistration(current, &webauthn.Credential{ID: []byte{id}, PublicKey: []byte{id + 1}}, old.Digest)
			results <- registrationResult{issue: issue, err: err}
		}(byte(99 + index*2))
	}
	var issue SessionIssue
	succeeded, refused := 0, 0
	for index := 0; index < 2; index++ {
		result := <-results
		if result.err == nil {
			succeeded++
			issue = result.issue
		} else if errors.Is(result.err, ErrInvitationGeneric) {
			refused++
		} else {
			t.Fatalf("registration race result = %v", result.err)
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("registration race succeeded=%d refused=%d", succeeded, refused)
	}
	if !slices.Equal(authority.added, []string{"New passkey"}) {
		t.Fatalf("durable registration reports=%v", authority.added)
	}
	WaitRuntimes(issue.Runtimes)
	if manager.Recheck(old) {
		t.Fatal("registration left the overwritten browser session active")
	}
	if identity, ok := manager.AuthenticateToken(issue.Token); !ok || identity.TrustID == old.TrustID {
		t.Fatalf("new registration session identity=%+v active=%t", identity, ok)
	}
	if _, err := manager.commitRegistration(current, &webauthn.Credential{ID: []byte{111}, PublicKey: []byte{112}}, ""); !errors.Is(err, ErrInvitationGeneric) {
		t.Fatalf("consumed invitation replay = %v", err)
	}
}

func writeTestStateJSON(t *testing.T, path string, state *State) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeTestStateBytes(t, path, data)
}

func writeTestStateBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRevocationPreservesFinalPasskeyAndResetIsScoped(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	manager, store, _, trustIDs := seededManager(t, clock, 2, "30d")
	authority := &testNotificationAuthority{}
	manager.SetNotificationAuthority(authority)
	origin := manager.state.PublicOrigin
	userHandle := manager.state.UserHandle
	results := make(chan error, 2)
	for _, trustID := range trustIDs {
		go func(id string) {
			result, err := manager.RevokeLocal(id)
			WaitRuntimes(result.Runtimes)
			results <- err
		}(trustID)
	}
	succeeded, refused := 0, 0
	for range trustIDs {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrLastCredential):
			refused++
		default:
			t.Fatalf("unexpected revoke result: %v", err)
		}
	}
	if succeeded != 1 || refused != 1 || activeCredentialCount(manager.state) != 1 {
		t.Fatalf("concurrent revokes succeeded=%d refused=%d active=%d", succeeded, refused, activeCredentialCount(manager.state))
	}
	if len(authority.removed) != 1 {
		t.Fatalf("revocation notification cleanups=%d, want 1", len(authority.removed))
	}
	if _, err := manager.RevokeLocal(manager.LocalDevices()[0].TrustID); !errors.Is(err, ErrLastCredential) {
		t.Fatalf("final local revoke = %v", err)
	}
	runtimes, err := manager.Reset()
	if err != nil {
		t.Fatal(err)
	}
	WaitRuntimes(runtimes)
	if manager.state.PublicOrigin != origin || manager.state.UserHandle == userHandle || len(manager.state.Credentials) != 0 || len(manager.state.Sessions) != 0 || len(manager.state.Invitations) != 0 {
		t.Fatalf("reset state = %+v", manager.state)
	}
	if authority.resets != 1 {
		t.Fatalf("access reset notification cleanups=%d, want 1", authority.resets)
	}
	path := store.path
	manager.Close()
	store.Close()
	restarted, err := OpenProtected(OpenOptions{Path: path, Now: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Store.Close()
	if restarted.BootstrapToken == "" || restarted.Store.state.PublicOrigin != origin {
		t.Fatalf("post-reset start bootstrap=%t origin=%q", restarted.BootstrapToken != "", restarted.Store.state.PublicOrigin)
	}
}

func seededManager(t *testing.T, clock Clock, credentialCount int, lifetime string) (*Manager, *Store, []string, []string) {
	t.Helper()
	result, err := OpenProtected(OpenOptions{
		Path: filepath.Join(t.TempDir(), "state", "access.json"), PublicOrigin: "https://shepherdr.private",
		SessionLifetime: lifetime, SessionLifetimeSet: true, Now: clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := result.Store.state
	state.Invitations = []InvitationRecord{}
	state.Credentials = []CredentialRecord{}
	state.Sessions = []SessionRecord{}
	tokens := make([]string, 0, credentialCount)
	trustIDs := make([]string, 0, credentialCount)
	parsedLifetime, _ := ParseSessionLifetime(lifetime)
	for index := 0; index < credentialCount; index++ {
		trustID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(index + 10)}, 16))
		token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(index + 30)}, 32))
		now := clock.Now()
		state.Credentials = append(state.Credentials, CredentialRecord{
			TrustID: trustID, Label: "Device", CreatedAt: now, LastUsedAt: now, BackupObservedAt: now,
			Credential: webauthn.Credential{ID: []byte{byte(index + 1)}, PublicKey: []byte{byte(index + 2)}},
		})
		session := SessionRecord{
			Digest: digestToken(token), TrustID: trustID, CreatedAt: now, Lifetime: lifetime,
			FreshTrustID: trustID, FreshAt: now,
		}
		if !parsedLifetime.None {
			expires := now.Add(parsedLifetime.Duration())
			session.ExpiresAt = &expires
		}
		state.Sessions = append(state.Sessions, session)
		tokens = append(tokens, token)
		trustIDs = append(trustIDs, trustID)
	}
	if err := result.Store.write(); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(result.Store, clock)
	if err != nil {
		t.Fatal(err)
	}
	return manager, result.Store, tokens, trustIDs
}

func fatalState(t *testing.T, message string) {
	t.Helper()
	t.Fatal(message)
}
