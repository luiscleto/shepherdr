package notifications

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
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
		if !publiclyRoutableIP(direct) {
			return nil, errors.New("subscription endpoint does not resolve to a publicly routable address")
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
		if !publiclyRoutableIP(address.IP) {
			return nil, errors.New("subscription endpoint does not resolve to a publicly routable address")
		}
		result = append(result, address.IP)
	}
	return result, nil
}

// nonPublicPrefixes records the IANA IPv4 and IPv6 special-purpose assignments
// that are not suitable for a public push service. See the current registries at
// https://www.iana.org/assignments/iana-ipv4-special-registry and
// https://www.iana.org/assignments/iana-ipv6-special-registry.
// Whole assignment blocks are rejected where their exceptional anycast or
// transition addresses are unnecessary for standard Web Push endpoints.
var nonPublicPrefixes = []netip.Prefix{
	// IPv4 this-host, private, shared, loopback, link-local, protocol,
	// documentation, deprecated transition, benchmarking, multicast, and reserved.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),

	// IPv6 compatible/mapped, translation, discard, protocol assignments,
	// documentation, transition, segment-routing, local, multicast, and reserved.
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var (
	allocatedIPv6GlobalUnicast = netip.MustParsePrefix("2000::/3")
	wellKnownNAT64Prefix       = netip.MustParsePrefix("64:ff9b::/96")
)

func publiclyRoutableIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	// IANA marks the well-known NAT64 prefix globally reachable. Apply the
	// IPv4 policy to its embedded destination so translation cannot bypass it.
	if wellKnownNAT64Prefix.Contains(address) {
		bytes := address.As16()
		address = netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]})
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	if !address.IsGlobalUnicast() {
		return false
	}
	if address.Is6() && !allocatedIPv6GlobalUnicast.Contains(address) {
		return false
	}
	return true
}

type contextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type safeDialer struct {
	validator *endpointValidator
	dialer    contextDialer
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
