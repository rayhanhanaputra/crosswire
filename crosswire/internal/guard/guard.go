// Package guard holds the destination and URL rules every outbound
// connection follows: no unspecified, multicast, link-local or reserved
// addresses, raw protocols limited to private networks, no credentials in
// URLs, and TLS required off-host for production targets. The checks pin the
// resolved address, so a name that resolves differently on a second lookup
// cannot reach somewhere the first lookup did not.
package guard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ClassifyIP returns an error for an address no target should ever be.
// When publicOK is false the address must also be loopback or on a private
// network (RFC 1918, unique local IPv6, or the 100.64.0.0/10 shared range
// that overlay networks such as NetBird and Tailscale use).
func ClassifyIP(ip net.IP, publicOK bool) error {
	switch {
	case ip.IsUnspecified():
		return errors.New("target resolves to an unspecified address")
	case ip.IsMulticast():
		return errors.New("target resolves to a multicast address")
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return errors.New("target resolves to a link-local address (this includes cloud metadata services)")
	case isReserved(ip):
		return errors.New("target resolves to a reserved address")
	}
	if !publicOK && !(ip.IsLoopback() || ip.IsPrivate() || isShared(ip)) {
		return errors.New("this protocol is limited to loopback and private networks")
	}
	return nil
}

func isReserved(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		return v4[0] >= 240 || v4[0] == 0
	}
	return false
}

func isShared(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 64
}

// Resolve looks the host up and returns the first acceptable address. Every
// address the name resolves to must pass, so a mixed answer is refused.
func Resolve(ctx context.Context, host string, publicOK bool) (net.IP, error) {
	host = strings.Trim(host, "[]")
	if host == "" {
		return nil, errors.New("target host is empty")
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve target host %q", host)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("target host has no usable address")
	}
	for _, ip := range ips {
		if err := ClassifyIP(ip, publicOK); err != nil {
			return nil, err
		}
	}
	return ips[0], nil
}

// DialContext returns a dial function that resolves, checks and then dials
// the pinned address. Use it as http.Transport.DialContext or for raw sockets.
func DialContext(publicOK bool, timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ip, err := Resolve(ctx, host, publicOK)
		if err != nil {
			return nil, err
		}
		d := net.Dialer{Timeout: timeout}
		return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
}

// IsLoopbackHost reports whether host is localhost or a loopback address.
func IsLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ParseURL parses an absolute URL with one of the schemes and refuses
// credentials and fragments in it.
func ParseURL(raw string, schemes ...string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("%q is not an absolute URL", raw)
	}
	ok := false
	for _, s := range schemes {
		if u.Scheme == s {
			ok = true
		}
	}
	if !ok {
		return nil, fmt.Errorf("URL scheme must be one of %s", strings.Join(schemes, ", "))
	}
	if u.User != nil || u.Fragment != "" {
		return nil, errors.New("URL cannot contain credentials or a fragment")
	}
	return u, nil
}

// RequireTLSOffHost enforces the production rule: any target that is not on
// this machine must use an encrypted scheme.
func RequireTLSOffHost(u *url.URL) error {
	switch u.Scheme {
	case "https", "wss", "mqtts":
		return nil
	}
	if IsLoopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("production target %s must use TLS (https, wss or mqtts) unless it is on this machine", u.Host)
}

// managedHeaders are set by the connector or carry credentials. A static
// header with one of these names is refused: credentials go through
// secret_headers, which hold the NAME of an environment variable.
var managedHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true, "connection": true,
	"authorization": true, "proxy-authorization": true, "x-api-key": true, "api-key": true,
	"cookie": true, "set-cookie": true,
}

// CheckStaticHeader refuses a plain-text header that is managed or secret.
func CheckStaticHeader(name, value string) error {
	if managedHeaders[strings.ToLower(strings.TrimSpace(name))] {
		return fmt.Errorf("header %q is managed by the connector or carries a credential; use secret_headers for credentials", name)
	}
	if strings.TrimSpace(name) == "" || len(name) > 128 || len(value) > 2000 {
		return errors.New("header names and values exceed their limits")
	}
	return nil
}

// CheckSecretHeaderName refuses a secret header that the connector manages.
func CheckSecretHeaderName(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "content-length", "transfer-encoding", "connection", "":
		return fmt.Errorf("secret header name %q is not allowed", name)
	}
	return nil
}
