package restic

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"backup-platform/internal/storage/s3"
)

// SecureResticProxy is a local loopback forward proxy dedicated to intercepting Restic S3 connections
// and strictly enforcing ADR-032 SSRF and DNS-rebinding security policies at connection time.
type SecureResticProxy struct {
	listener net.Listener
	policy   *s3.EndpointSecurityPolicy
	port     int
	mu       sync.Mutex
	closed   bool
	conns    map[net.Conn]struct{}
}

// StartSecureResticProxy starts a new SecureResticProxy on loopback 127.0.0.1 with an ephemeral port.
func StartSecureResticProxy(policy *s3.EndpointSecurityPolicy) (*SecureResticProxy, error) {
	if policy == nil {
		policy = &s3.EndpointSecurityPolicy{}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed binding local secure proxy listener: %w", err)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("unexpected listener address type: %T", listener.Addr())
	}

	proxy := &SecureResticProxy{
		listener: listener,
		policy:   policy,
		port:     tcpAddr.Port,
		conns:    make(map[net.Conn]struct{}),
	}

	go proxy.serve()

	return proxy, nil
}

// Port returns the bound TCP port on 127.0.0.1.
func (p *SecureResticProxy) Port() int {
	return p.port
}

// Env returns the environment variables routing child subprocess HTTP/HTTPS traffic through this proxy.
func (p *SecureResticProxy) Env() []string {
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)
	return []string{
		"HTTP_PROXY=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"ALL_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"https_proxy=" + proxyURL,
		"all_proxy=" + proxyURL,
		"NO_PROXY=",
		"no_proxy=",
	}
}

// Close gracefully stops the proxy listener and terminates active connections.
func (p *SecureResticProxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	err := p.listener.Close()
	for c := range p.conns {
		_ = c.Close()
	}
	p.conns = make(map[net.Conn]struct{})
	p.mu.Unlock()
	return err
}

func (p *SecureResticProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			p.mu.Lock()
			isClosed := p.closed
			p.mu.Unlock()
			if isClosed {
				return
			}
			continue
		}

		p.mu.Lock()
		if p.closed {
			_ = conn.Close()
			p.mu.Unlock()
			return
		}
		p.conns[conn] = struct{}{}
		p.mu.Unlock()

		go p.handleConn(conn)
	}
}

func (p *SecureResticProxy) handleConn(clientConn net.Conn) {
	defer func() {
		_ = clientConn.Close()
		p.mu.Lock()
		delete(p.conns, clientConn)
		p.mu.Unlock()
	}()

	br := bufio.NewReader(clientConn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}

	if req.Method == http.MethodConnect {
		p.handleConnect(clientConn, req)
		return
	}

	p.handleHTTP(clientConn, req)
}

func (p *SecureResticProxy) handleConnect(clientConn net.Conn, req *http.Request) {
	targetHostPort := req.RequestURI
	host, port, err := net.SplitHostPort(targetHostPort)
	if err != nil {
		host = targetHostPort
		port = "443"
		targetHostPort = net.JoinHostPort(host, port)
	}

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer dialCancel()

	secureClient := p.policy.NewSecureHTTPClient()
	transport, ok := secureClient.Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		resp := "HTTP/1.1 500 Internal Server Error\r\nConnection: close\r\n\r\n"
		_, _ = clientConn.Write([]byte(resp))
		return
	}

	// Dial using the policy's secure connection-time validator to resolve host and defeat DNS rebinding
	targetConn, err := transport.DialContext(dialCtx, "tcp", targetHostPort)
	if err != nil {
		resp := "HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nBlocked by SSRF policy\n"
		_, _ = clientConn.Write([]byte(resp))
		return
	}
	defer targetConn.Close()

	// Connection allowed and established
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	// Stream TLS payload bidirectionally
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(targetConn, clientConn)
		if tc, ok := targetConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(clientConn, targetConn)
		if cc, ok := clientConn.(*net.TCPConn); ok {
			_ = cc.CloseWrite()
		}
	}()

	wg.Wait()
}

func (p *SecureResticProxy) handleHTTP(clientConn net.Conn, req *http.Request) {
	if !p.policy.AllowInsecureHTTP {
		resp := "HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nInsecure HTTP not allowed by policy\n"
		_, _ = clientConn.Write([]byte(resp))
		return
	}

	client := p.policy.NewSecureHTTPClient()

	outReq, err := http.NewRequestWithContext(req.Context(), req.Method, req.RequestURI, req.Body)
	if err != nil {
		_, _ = clientConn.Write([]byte("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n"))
		return
	}
	outReq.Header = req.Header.Clone()
	outReq.Header.Del("Proxy-Connection")

	resp, err := client.Do(outReq)
	if err != nil {
		respMsg := "HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nBlocked by SSRF policy\n"
		_, _ = clientConn.Write([]byte(respMsg))
		return
	}
	defer resp.Body.Close()

	_ = resp.Write(clientConn)
}
