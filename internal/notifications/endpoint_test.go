package notifications

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
)

type fixedResolver struct {
	addresses []net.IPAddr
	err       error
}

func (r fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, r.err
}

func TestPublicRoutabilityRejectsSpecialPurposeRegistries(t *testing.T) {
	for _, test := range []struct {
		address string
		public  bool
	}{
		{address: "8.8.8.8", public: true},
		{address: "1.1.1.1", public: true},
		{address: "0.1.2.3"},
		{address: "10.0.0.1"},
		{address: "100.100.20.4"},
		{address: "127.0.0.1"},
		{address: "169.254.1.1"},
		{address: "172.16.0.1"},
		{address: "192.0.0.1"},
		{address: "192.0.2.1"},
		{address: "192.88.99.1"},
		{address: "192.168.1.8"},
		{address: "198.18.0.1"},
		{address: "198.51.100.1"},
		{address: "203.0.113.1"},
		{address: "224.0.0.1"},
		{address: "240.0.0.1"},
		{address: "255.255.255.255"},
		{address: "2001:4860:4860::8888", public: true},
		{address: "2606:4700:4700::1111", public: true},
		{address: "::"},
		{address: "::1"},
		{address: "::ffff:127.0.0.1"},
		{address: "64:ff9b::808:808", public: true},
		{address: "64:ff9b::7f00:1"},
		{address: "64:ff9b::a00:1"},
		{address: "64:ff9b:1::1"},
		{address: "100::1"},
		{address: "100:0:0:1::1"},
		{address: "2001::1"},
		{address: "2001:db8::1"},
		{address: "2002:808:808::1"},
		{address: "3fff::1"},
		{address: "5f00::1"},
		{address: "4000::1"},
		{address: "fc00::1"},
		{address: "fec0::1"},
		{address: "fe80::1"},
		{address: "ff00::1"},
	} {
		t.Run(test.address, func(t *testing.T) {
			if got := publiclyRoutableIP(net.ParseIP(test.address)); got != test.public {
				t.Fatalf("publiclyRoutableIP(%q) = %t, want %t", test.address, got, test.public)
			}
		})
	}
}

func TestEndpointValidationRejectsAnyNonPublicDNSAnswer(t *testing.T) {
	for _, test := range []struct {
		name      string
		addresses []string
		wantError bool
	}{
		{name: "public", addresses: []string{"8.8.8.8"}},
		{name: "benchmarking", addresses: []string{"198.18.0.1"}, wantError: true},
		{name: "documentation", addresses: []string{"2001:db8::1"}, wantError: true},
		{name: "mixed IPv4", addresses: []string{"8.8.8.8", "198.51.100.2"}, wantError: true},
		{name: "mixed IPv6", addresses: []string{"2606:4700:4700::1111", "64:ff9b:1::1"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			addresses := make([]net.IPAddr, len(test.addresses))
			for index, value := range test.addresses {
				addresses[index].IP = net.ParseIP(value)
			}
			validator := newEndpointValidator(fixedResolver{addresses: addresses})
			err := validator.validate(context.Background(), "https://push.example/send")
			if (err != nil) != test.wantError {
				t.Fatalf("validation error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

type recordingDialer struct {
	mu        sync.Mutex
	addresses []string
}

func (d *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addresses = append(d.addresses, address)
	d.mu.Unlock()
	return nil, errors.New("bounded dial fixture")
}

func (d *recordingDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.addresses)
}

func (d *recordingDialer) lastAddress() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.addresses) == 0 {
		return ""
	}
	return d.addresses[len(d.addresses)-1]
}

func TestPinnedDialerRechecksPublicRoutabilityBeforeConnecting(t *testing.T) {
	dialer := &recordingDialer{}
	validator := newEndpointValidator(fixedResolver{addresses: []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("198.18.0.1")},
	}})
	safe := &safeDialer{validator: validator, dialer: dialer}
	if _, err := safe.DialContext(context.Background(), "tcp", "push.example:443"); err == nil {
		t.Fatal("mixed public and non-public send-time DNS unexpectedly dialed")
	}
	if got := dialer.count(); got != 0 {
		t.Fatalf("unsafe send-time DNS reached the network dialer %d times", got)
	}

	publicDialer := &recordingDialer{}
	publicValidator := newEndpointValidator(fixedResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}})
	publicSafe := &safeDialer{validator: publicValidator, dialer: publicDialer}
	_, _ = publicSafe.DialContext(context.Background(), "tcp", "push.example:443")
	if got := publicDialer.count(); got != 1 || publicDialer.lastAddress() != "8.8.8.8:443" {
		t.Fatalf("public send-time DNS dial count=%d address=%q", got, publicDialer.lastAddress())
	}
}

func TestEndpointValidationRequiresStrictHTTPSShapeAndResolution(t *testing.T) {
	validator := newEndpointValidator(fixedResolver{err: errors.New("dns failed")})
	for _, endpoint := range []string{
		"http://push.example/send",
		"https://user@push.example/send",
		"https://push.example/send#fragment",
		"https://127.0.0.1/send",
		"https://push.example/send",
	} {
		if err := validator.validate(context.Background(), endpoint); err == nil {
			t.Errorf("endpoint %q unexpectedly passed", endpoint)
		}
	}
}

func TestManagerSaveRequiresStrictSubscriptionAndPublicResolution(t *testing.T) {
	store, err := OpenStore(t.TempDir()+"/notifications.json", "mailto:operator@example.com")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, nil)
	manager.validator = newEndpointValidator(fixedResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}})
	valid := validSubscription(t, "https://push.example/send", EventSettings{Blocked: true})
	browser := BrowserSubscription{Endpoint: valid.Endpoint, Keys: valid.Keys}
	if err := manager.Save(context.Background(), browser, valid.Events); err != nil {
		t.Fatalf("valid browser subscription was refused: %v", err)
	}
	if settings, found, err := store.Lookup(valid.Endpoint); err != nil || !found || settings != valid.Events {
		t.Fatalf("saved browser = %+v found=%t err=%v", settings, found, err)
	}

	invalidKey := browser
	invalidKey.Keys.Auth = "short"
	if err := manager.Save(context.Background(), invalidKey, valid.Events); err == nil {
		t.Fatal("invalid browser key was accepted")
	}
	negative := float64(-1)
	invalidExpiry := browser
	invalidExpiry.ExpirationTime = &negative
	if err := manager.Save(context.Background(), invalidExpiry, valid.Events); err == nil {
		t.Fatal("negative expiry was accepted")
	}
	manager.validator = newEndpointValidator(fixedResolver{addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.4")}}})
	if err := manager.Save(context.Background(), browser, valid.Events); err == nil {
		t.Fatal("private push destination was accepted")
	}
}
