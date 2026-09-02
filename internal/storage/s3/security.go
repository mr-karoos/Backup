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

// Predefined blocked IP subnets (loopback, link-local, cloud metadata, multicast, unspecified).
var blockedSubnets []*net.IPNet

// RFC 1918 and ULA private subnets.
var privateSubnets []*net.IPNet

func init() {
	blockedCIDRs := []string{
		"127.0.0.0/8",    // IPv4 Loopback
		"::1/128",        // IPv6 Loopback
		"169.254.0.0/16", // IPv4 Link-Local & Cloud Metadata (169.254.169.254)
		"fe80::/10",      // IPv6 Link-Local
		"224.0.0.0/4",    // IPv4 Multicast
		"ff00::/8",       // IPv6 Multicast
		"0.0.0.0/8",      // Current network
		"::/128",         // IPv6 Unspecified
	}
	for _, cidr := range blockedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			blockedSubnets = append(blockedSubnets, ipNet)
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

// ValidateEndpointURL checks syntax, scheme, userinfo, and host.
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

	return u, nil
}

// IsIPAllowed evaluates whether a resolved IP address is permissible.
func (p *EndpointSecurityPolicy) IsIPAllowed(host string, ip net.IP) bool {
	if ip == nil {
		return false
	}

	// 1. Check unconditionally blocked ranges (loopback, link-local, multicast, metadata, unspecified)
	for _, subnet := range blockedSubnets {
		if subnet.Contains(ip) {
			return false
		}
	}

	// 2. Check private RFC 1918 / ULA ranges
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

	// 3. If private, check against explicit private allowlist
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
// and disabled redirects.
func (p *EndpointSecurityPolicy) NewSecureHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
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
