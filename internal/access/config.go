package access

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSessionLifetime = "30d"
	MaxSessionDays         = 365
)

type SessionLifetime struct {
	Days int
	None bool
}

func (l SessionLifetime) String() string {
	if l.None {
		return "none"
	}
	return strconv.Itoa(l.Days) + "d"
}

func (l SessionLifetime) Duration() time.Duration {
	if l.None {
		return 0
	}
	return time.Duration(l.Days) * 24 * time.Hour
}

func ParseSessionLifetime(value string) (SessionLifetime, error) {
	if value == "none" {
		return SessionLifetime{None: true}, nil
	}
	if len(value) < 2 || value[len(value)-1] != 'd' {
		return SessionLifetime{}, errors.New("-session-lifetime must be none or a whole number of days from 1d through 365d")
	}
	daysText := value[:len(value)-1]
	if strings.HasPrefix(daysText, "+") || len(daysText) > 3 || len(daysText) > 1 && daysText[0] == '0' {
		return SessionLifetime{}, errors.New("-session-lifetime must be none or a whole number of days from 1d through 365d")
	}
	for _, character := range daysText {
		if character < '0' || character > '9' {
			return SessionLifetime{}, errors.New("-session-lifetime must be none or a whole number of days from 1d through 365d")
		}
	}
	days, err := strconv.Atoi(daysText)
	if err != nil || days < 1 || days > MaxSessionDays {
		return SessionLifetime{}, errors.New("-session-lifetime must be none or a whole number of days from 1d through 365d")
	}
	return SessionLifetime{Days: days}, nil
}

type Origin struct {
	Authority string
	RPID      string
	Value     string
}

func ParseCanonicalOrigin(value string) (Origin, error) {
	invalid := func() (Origin, error) {
		return Origin{}, errors.New("-public-origin must be exactly https://lowercase-ascii-dns-name[:nondefault-port] without a path")
	}
	if value == "" || strings.TrimSpace(value) != value {
		return invalid()
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return invalid()
	}
	hostname := parsed.Hostname()
	if hostname == "" || hostname != strings.ToLower(hostname) || strings.HasSuffix(hostname, ".") || !validASCIIDNSName(hostname) ||
		net.ParseIP(hostname) != nil || looksLikeIPv4Number(hostname) {
		return invalid()
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "localhost" {
			return invalid()
		}
	}
	port := parsed.Port()
	if port == "443" {
		return invalid()
	}
	authority := hostname
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 || strconv.Itoa(portNumber) != port {
			return invalid()
		}
		authority += ":" + port
	}
	canonical := "https://" + authority
	if canonical != value || parsed.Host != authority {
		return invalid()
	}
	return Origin{Authority: authority, RPID: hostname, Value: value}, nil
}

func looksLikeIPv4Number(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		base := 10
		digits := part
		if len(part) > 2 && part[0:2] == "0x" {
			base = 16
			digits = part[2:]
		} else if len(part) > 1 && part[0] == '0' {
			base = 8
			digits = part[1:]
		}
		if digits == "" {
			return false
		}
		if _, err := strconv.ParseUint(digits, base, 32); err != nil {
			return false
		}
	}
	return true
}

func validASCIIDNSName(name string) bool {
	if len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func (o Origin) ValidateHost(host string) bool {
	return host == o.Authority
}

func (o Origin) ValidateOriginHeader(values []string) bool {
	if len(values) != 1 || values[0] != o.Value {
		return false
	}
	parsed, err := ParseCanonicalOrigin(values[0])
	return err == nil && parsed == o
}

func (o Origin) InvitationURL(token string) string {
	return fmt.Sprintf("%s/#trust=%s", o.Value, token)
}
