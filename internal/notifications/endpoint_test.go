package notifications

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fixedResolver struct {
	addresses []net.IPAddr
	err       error
}

func (r fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, r.err
}

func TestEndpointValidationRejectsLocalPrivateTailnetAndMixedDNS(t *testing.T) {
	for _, test := range []struct {
		name      string
		addresses []string
		wantError bool
	}{
		{name: "public", addresses: []string{"8.8.8.8"}},
		{name: "loopback", addresses: []string{"127.0.0.1"}, wantError: true},
		{name: "private", addresses: []string{"192.168.1.8"}, wantError: true},
		{name: "link local", addresses: []string{"169.254.1.1"}, wantError: true},
		{name: "tailnet", addresses: []string{"100.100.20.4"}, wantError: true},
		{name: "mixed rebinding", addresses: []string{"8.8.8.8", "10.0.0.2"}, wantError: true},
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
