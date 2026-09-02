package s3

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrInsecureScheme indicates HTTP was used without permission.
	ErrInsecureScheme = errors.New("s3 endpoint scheme must be https")

	// ErrBlockedAddress indicates an attempt to connect to a blocked IP/range (SSRF).
	ErrBlockedAddress = errors.New("s3 endpoint resolves to a blocked or restricted network address")

	// ErrRedirectNotAllowed indicates an HTTP redirect was attempted.
	ErrRedirectNotAllowed = errors.New("http redirects are not allowed for s3 endpoints")
)

// Predefined unconditionally blocked IP subnets (link-local, cloud metadata, multicast, unspecified).
var unconditionallyBlockedSubnets []*net.IPNet

// Loopback subnets (127.0.0.0/8, ::1/128). Blocked by default.
var loopbackSubnets []*net.IPNet

// RFC 1918 and ULA private subnets.
var privateSubnets []*net.IPNet

func init() {
	unconditionallyBlockedCIDRs := []string{
		"169.254.0.0/16", // IPv4 Link-Local & Cloud Metadata (169.254.169.254)
		"fe80::/10",      // IPv6 Link-Local
		"224.0.0.0/4",    // IPv4 Multicast
		"ff00::/8",       // IPv6 Multicast
		"0.0.0.0/8",      // Current network
		"::/128",         // IPv6 Unspecified
	}
	for _, cidr := range unconditionallyBlockedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			unconditionallyBlockedSubnets = append(unconditionallyBlockedSubnets, ipNet)
		}
	}

	loopbackCIDRs := []string{
		"127.0.0.0/8", // IPv4 Loopback
		"::1/128",     // IPv6 Loopback
	}
	for _, cidr := range loopbackCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			loopbackSubnets = append(loopbackSubnets, ipNet)
		}
	}

	privateCIDRs := []string{
		"10.0.0.0/8",     // RFC 1918 Class A
		"172.16.0.0/12",  // RFC 1918 Class B
		"192.168.0.0/16", // RFC 1918 Class C
		"fc00::/7",       // IPv6 Unique Local Address (ULA)
	}
	for _, cidr := range privateCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			privateSubnets = append(privateSubnets, ipNet)
		}
	}
}

// EndpointSecurityPolicy enforces SSRF protection, URL validation, and connection-time IP validation.
type EndpointSecurityPolicy struct {
	AllowInsecureHTTP bool
	PrivateAllowlist  []string // List of allowed hostnames, IP strings, or CIDR blocks
}

// ValidateEndpointURL checks syntax, scheme, userinfo, host, path, query, and fragment.
func (p *EndpointSecurityPolicy) ValidateEndpointURL(endpointURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(endpointURL)
	if trimmed == "" {
		return nil, nil // Default AWS endpoint
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return nil, errors.New("invalid s3 endpoint URL")
	}

	if u.User != nil {
		return nil, errors.New("s3 endpoint must not contain user credentials")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" {
		if scheme == "http" {
			if !p.AllowInsecureHTTP {
				return nil, ErrInsecureScheme
			}
		} else {
			return nil, fmt.Errorf("unsupported s3 endpoint scheme: %s", scheme)
		}
	}

	// Path must be empty or root "/"
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("s3 endpoint URL must not contain a path component")
	}

	// Reject query parameters and URL fragments fail-closed
	if u.RawQuery != "" {
		return nil, errors.New("s3 endpoint URL must not contain query parameters")
	}
	if u.Fragment != "" {
		return nil, errors.New("s3 endpoint URL must not contain a fragment")
	}

	return u, nil
}

// IsIPAllowed evaluates whether a resolved IP address is permissible.
func (p *EndpointSecurityPolicy) IsIPAllowed(host string, ip net.IP) bool {
	if ip == nil {
		return false
	}

	// 1. Unconditionally blocked ranges (cloud metadata, link-local, multicast, unspecified).
	// These can NEVER be bypassed or allowlisted under any circumstances.
	for _, subnet := range unconditionallyBlockedSubnets {
		if subnet.Contains(ip) {
			return false
		}
	}

	// 2. Loopback ranges (127.0.0.0/8, ::1/128).
	// Blocked by default. Allowed ONLY in development/test when BOTH AllowInsecureHTTP is true AND explicitly allowlisted.
	isLoopback := false
	for _, subnet := range loopbackSubnets {
		if subnet.Contains(ip) {
			isLoopback = true
			break
		}
	}
	if isLoopback {
		if !p.AllowInsecureHTTP {
			return false
		}
		return p.matchesAllowlist(host, ip)
	}

	// 3. Check private RFC 1918 / ULA ranges
	isPrivate := false
	for _, subnet := range privateSubnets {
		if subnet.Contains(ip) {
			isPrivate = true
			break
		}
	}

	if !isPrivate {
		return true // Public routable IP
	}

	// 4. If private, check against explicit private allowlist
	return p.matchesAllowlist(host, ip)
}

func (p *EndpointSecurityPolicy) matchesAllowlist(host string, ip net.IP) bool {
	for _, allowed := range p.PrivateAllowlist {
		clean := strings.TrimSpace(allowed)
		if clean == "" {
			continue
		}

		// Check if allowlist entry matches hostname directly
		if strings.EqualFold(host, clean) {
			return true
		}

		// Check if allowlist entry is a CIDR block containing the IP
		if _, ipNet, err := net.ParseCIDR(clean); err == nil {
			if ipNet.Contains(ip) {
				return true
			}
		}

		// Check if allowlist entry is an exact IP match
		if parsedIP := net.ParseIP(clean); parsedIP != nil {
			if parsedIP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// NewSecureHTTPClient returns an *http.Client with connection-time DNS resolution, SSRF validation,
// explicit direct transport (no environment proxies), and disabled redirects.
func (p *EndpointSecurityPolicy) NewSecureHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy:                 nil, // Explicit direct transport; MUST NOT inherit HTTP_PROXY / HTTPS_PROXY / ALL_PROXY
		DialContext:           p.makeSecureDialContext(dialer),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return ErrRedirectNotAllowed
		},
	}
}

func (p *EndpointSecurityPolicy) makeSecureDialContext(baseDialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid network address '%s': %w", addr, err)
		}

		// Resolve IP addresses at connect time
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("failed resolving s3 host '%s': %w", host, err)
		}

		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses resolved for s3 host '%s'", host)
		}

		// Find the first permissible IP
		var targetIP net.IP
		for _, ipAddr := range ips {
			if p.IsIPAllowed(host, ipAddr.IP) {
				targetIP = ipAddr.IP
				break
			}
		}

		if targetIP == nil {
			return nil, fmt.Errorf("%w: host '%s' resolved to disallowed IP(s)", ErrBlockedAddress, host)
		}

		// Connect directly to the validated IP to prevent DNS rebinding
		directAddr := net.JoinHostPort(targetIP.String(), port)
		return baseDialer.DialContext(ctx, network, directAddr)
	}
}
