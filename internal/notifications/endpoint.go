package notifications

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const maxEndpointLength = 2048

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type endpointValidator struct {
	resolver ipResolver
}

func newEndpointValidator(resolver ipResolver) *endpointValidator {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &endpointValidator{resolver: resolver}
}

func validateEndpointURL(endpoint string) (*url.URL, error) {
	if endpoint == "" || len(endpoint) > maxEndpointLength || strings.TrimSpace(endpoint) != endpoint {
		return nil, errors.New("invalid subscription endpoint")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("subscription endpoint must be an HTTPS URL without credentials or a fragment")
	}
	if parsed.RawQuery != "" && len(parsed.RawQuery) > 1024 {
		return nil, errors.New("subscription endpoint query is too large")
	}
	return parsed, nil
}

func (v *endpointValidator) validate(ctx context.Context, endpoint string) error {
	parsed, err := validateEndpointURL(endpoint)
	if err != nil {
		return err
	}
	_, err = v.safeAddresses(ctx, parsed.Hostname())
	return err
}

func (v *endpointValidator) safeAddresses(ctx context.Context, hostname string) ([]net.IP, error) {
	if direct := net.ParseIP(hostname); direct != nil {
		if unsafeDestination(direct) {
			return nil, errors.New("subscription endpoint resolves to a private or local address")
		}
		return []net.IP{direct}, nil
	}
	addresses, err := v.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve subscription endpoint: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("subscription endpoint has no addresses")
	}
	result := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if unsafeDestination(address.IP) {
			return nil, errors.New("subscription endpoint resolves to a private or local address")
		}
		result = append(result, address.IP)
	}
	return result, nil
}

func unsafeDestination(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		// Tailnets and other shared-address-space destinations are not public push services.
		return ipv4[0] == 100 && ipv4[1]&0xc0 == 0x40
	}
	return false
}

type safeDialer struct {
	validator *endpointValidator
	dialer    net.Dialer
}

func (d *safeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("push request has an invalid destination")
	}
	addresses, err := d.validator.safeAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range addresses {
		connection, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
