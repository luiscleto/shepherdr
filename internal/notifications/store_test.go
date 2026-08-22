package notifications

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExistingSubscriptionsAndOlderSettingsEnableAccessChanges(t *testing.T) {
	var decoded EventSettings
	if err := json.Unmarshal([]byte(`{"blocked":true,"done":true}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.TrustedSignInAdded || !decoded.TrustedSignInRemoved {
		t.Fatalf("older browser settings did not safely default access changes on: %+v", decoded)
	}

	path := filepath.Join(t.TempDir(), "notifications.json")
	store, err := OpenStore(path, "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	subscription := validSubscription(t, "https://push.example/existing", EventSettings{Blocked: true})
	if err := store.Upsert(subscription); err != nil {
		t.Fatal(err)
	}
	prior := cloneState(store.state)
	prior.Version = ownedStateVersion
	prior.Subscriptions[0].Events.TrustedSignInAdded = false
	prior.Subscriptions[0].Events.TrustedSignInRemoved = false
	if err := writeState(path, prior); err != nil {
		t.Fatal(err)
	}

	restarted, err := OpenStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	settings, found, err := restarted.Lookup(subscription.Endpoint)
	if err != nil || !found || !settings.TrustedSignInAdded || !settings.TrustedSignInRemoved {
		t.Fatalf("migrated settings=%+v found=%t err=%v", settings, found, err)
	}
	if restarted.state.Version != stateVersion {
		t.Fatalf("migrated version=%d, want %d", restarted.state.Version, stateVersion)
	}
}

func TestProtectedMigrationOwnershipAndResetPreserveVAPIDIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	store, err := OpenStore(path, "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	legacy := validSubscription(t, "https://push.example/legacy", EventSettings{Blocked: true})
	if err := store.Upsert(legacy); err != nil {
		t.Fatal(err)
	}
	beforeContact, beforeKey, _, _ := store.Config()
	legacyState := cloneState(store.state)
	legacyState.Version = legacyStateVersion
	if err := writeState(path, legacyState); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	trustOne := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	trustTwoBytes := make([]byte, 16)
	trustTwoBytes[0] = 2
	trustTwo := base64.RawURLEncoding.EncodeToString(trustTwoBytes)
	if err := restarted.EnableProtected(map[string]struct{}{trustOne: {}, trustTwo: {}}); err != nil {
		t.Fatal(err)
	}
	if restarted.state.Version != stateVersion || len(restarted.state.Subscriptions) != 0 {
		t.Fatalf("protected migration state=%+v", restarted.state)
	}
	one := validSubscription(t, "https://push.example/one-owned", EventSettings{Done: true})
	two := validSubscription(t, "https://push.example/two-owned", EventSettings{Blocked: true})
	if err := restarted.UpsertOwned(one, trustOne); err != nil {
		t.Fatal(err)
	}
	if err := restarted.UpsertOwned(two, trustTwo); err != nil {
		t.Fatal(err)
	}
	if _, found, err := restarted.LookupOwned(one.Endpoint, trustTwo); err != nil || found {
		t.Fatalf("other owner lookup found=%t err=%v", found, err)
	}
	if err := restarted.UpsertOwned(one, trustTwo); err == nil {
		t.Fatal("another trusted sign-in claimed an existing endpoint")
	}
	if err := restarted.RemoveTrustSubscriptions(trustOne); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := restarted.LookupOwned(one.Endpoint, trustOne); found {
		t.Fatal("revoked owner subscription remained")
	}
	if _, found, _ := restarted.LookupOwned(two.Endpoint, trustTwo); !found {
		t.Fatal("unrelated owner subscription was removed")
	}
	if err := restarted.ResetProtectedSubscriptions(); err != nil {
		t.Fatal(err)
	}
	afterContact, afterKey, configured, err := restarted.Config()
	if err != nil || !configured || afterContact != beforeContact || afterKey != beforeKey || len(restarted.state.Subscriptions) != 0 {
		t.Fatalf("protected reset contact=%q key_same=%t configured=%t subscriptions=%d err=%v", afterContact, afterKey == beforeKey, configured, len(restarted.state.Subscriptions), err)
	}
}

func TestStorePersistsContactKeysAndIndependentBrowserSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "notifications.json")
	unconfigured, err := OpenStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, configured, err := unconfigured.Config(); err != nil || configured {
		t.Fatalf("unconfigured store: configured=%t err=%v", configured, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("opening without a contact created state: %v", err)
	}

	first, err := OpenStore(path, "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	contact, publicKey, configured, err := first.Config()
	if err != nil || !configured || contact != "mailto:operator@example.com" || publicKey == "" {
		t.Fatalf("configured store = contact %q key %q configured %t err %v", contact, publicKey, configured, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || parent.Mode().Perm() != 0o700 {
		t.Fatalf("state directory mode = %v err=%v, want 700", parent.Mode().Perm(), err)
	}

	one := validSubscription(t, "https://push.example/one", EventSettings{Blocked: true})
	two := validSubscription(t, "https://push.example/two", EventSettings{Done: true, WorkspaceClosed: true})
	if err := first.Upsert(one); err != nil {
		t.Fatal(err)
	}
	if err := first.Upsert(two); err != nil {
		t.Fatal(err)
	}

	restarted, err := OpenStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	restartedContact, restartedKey, configured, err := restarted.Config()
	if err != nil || !configured || restartedContact != contact || restartedKey != publicKey {
		t.Fatalf("restart changed installation state: contact=%q key=%q configured=%t err=%v", restartedContact, restartedKey, configured, err)
	}
	if settings, found, err := restarted.Lookup(two.Endpoint); err != nil || !found || settings != two.Events {
		t.Fatalf("second browser settings = %+v found=%t err=%v", settings, found, err)
	}

	updated, err := OpenStore(path, "https://operator.example/contact")
	if err != nil {
		t.Fatal(err)
	}
	updatedContact, updatedKey, _, _ := updated.Config()
	if updatedContact != "https://operator.example/contact" || updatedKey != publicKey {
		t.Fatalf("contact update rotated identity: contact=%q key=%q", updatedContact, updatedKey)
	}
	if _, found, _ := updated.Lookup(one.Endpoint); !found {
		t.Fatal("contact update removed a browser subscription")
	}
	persistedUpdate, err := OpenStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	persistedContact, persistedKey, _, _ := persistedUpdate.Config()
	if persistedContact != "https://operator.example/contact" || persistedKey != publicKey {
		t.Fatalf("updated contact did not persist without rotation: contact=%q key=%q", persistedContact, persistedKey)
	}

	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("reset left notification state: %v", err)
	}
	afterReset, err := OpenStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, configured, err := afterReset.Config(); err != nil || configured {
		t.Fatalf("reset state configured=%t err=%v, want unavailable until contact is supplied", configured, err)
	}
}

func TestResetRemovesOnlyStateAndRecognizedTemporaryResidue(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "notifications.json")
	if _, err := OpenStore(path, "mailto:operator@example.com"); err != nil {
		t.Fatal(err)
	}
	residue, err := os.CreateTemp(directory, ".notifications-*")
	if err != nil {
		t.Fatal(err)
	}
	residueName := residue.Name()
	if _, err := residue.WriteString("old notification secrets"); err != nil {
		t.Fatal(err)
	}
	if err := residue.Close(); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(directory, ".notifications-backup")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelatedNumeric := filepath.Join(directory, ".notifications-123456")
	if err := os.WriteFile(unrelatedNumeric, []byte("also keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unrelatedNumeric, 0o644); err != nil {
		t.Fatal(err)
	}
	unrelatedDirectory := filepath.Join(directory, ".notifications-123")
	if err := os.Mkdir(unrelatedDirectory, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := Reset(path); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{path, residueName} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("reset left %q: %v", removed, err)
		}
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatalf("reset changed unrelated file: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(unrelatedNumeric); err != nil || string(data) != "also keep" {
		t.Fatalf("reset changed non-secret numeric sibling: data=%q err=%v", data, err)
	}
	if info, err := os.Stat(unrelatedDirectory); err != nil || !info.IsDir() {
		t.Fatalf("reset changed unrelated directory: info=%v err=%v", info, err)
	}
}

func TestValidateContactRequiresRealMailtoOrHTTPSURI(t *testing.T) {
	valid := []string{"mailto:operator@example.com", "https://operator.example/contact"}
	for _, value := range valid {
		if got, err := ValidateContact(value); err != nil || got != value {
			t.Errorf("ValidateContact(%q) = %q, %v", value, got, err)
		}
	}
	invalid := []string{"", "operator@example.com", "mailto:", "mailto:Name <operator@example.com>", "http://operator.example", "https://user@operator.example", "https://operator.example/#fragment"}
	for _, value := range invalid {
		if _, err := ValidateContact(value); err == nil {
			t.Errorf("ValidateContact(%q) unexpectedly succeeded", value)
		}
	}
	if _, err := OpenStore(filepath.Join(t.TempDir(), "notifications.json"), "http://operator.example"); err == nil {
		t.Fatal("store accepted an invalid supplied contact")
	}
}

func TestCorruptStoreStaysUnavailableUntilReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path, "mailto:operator@example.com")
	if err == nil {
		t.Fatal("corrupt state unexpectedly loaded")
	}
	if _, _, configured, configErr := store.Config(); configErr == nil || configured {
		t.Fatalf("corrupt store remained configured=%t err=%v", configured, configErr)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != "{not-json" {
		t.Fatalf("corrupt state was silently replaced: %q err=%v", data, readErr)
	}
}

func TestInvalidPrivateScalarDisablesStoreWithoutPanicking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	store, err := OpenStore(path, "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	invalid := cloneState(store.state)
	invalid.VAPIDPrivateKey = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if err := writeState(path, invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path, ""); err == nil {
		t.Fatal("zero VAPID private scalar unexpectedly loaded")
	}
}

func validSubscription(t *testing.T, endpoint string, events EventSettings) Subscription {
	t.Helper()
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return Subscription{
		Endpoint: endpoint,
		Keys: SubscriptionKeys{
			Auth:   base64.RawURLEncoding.EncodeToString(auth),
			P256dh: base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		},
		Events: events,
	}
}
