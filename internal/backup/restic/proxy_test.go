package restic

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"backup-platform/internal/storage/s3"
)

func TestSecureResticProxy_SSRFProtection(t *testing.T) {
	policy := &s3.EndpointSecurityPolicy{
		AllowInsecureHTTP: false,
		PrivateAllowlist:  []string{"127.0.0.1"},
	}

	proxy, err := StartSecureResticProxy(policy)
	if err != nil {
		t.Fatalf("failed starting proxy: %v", err)
	}
	defer proxy.Close()

	t.Run("CONNECT to link-local 169.254.169.254 is blocked with 403", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxy.Port()))
		if err != nil {
			t.Fatalf("failed dialing proxy: %v", err)
		}
		defer conn.Close()

		req := "CONNECT 169.254.169.254:443 HTTP/1.1\r\nHost: 169.254.169.254:443\r\n\r\n"
		if _, err := conn.Write([]byte(req)); err != nil {
			t.Fatalf("failed writing request: %v", err)
		}

		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("failed reading response: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("CONNECT to un-allowlisted private IP 10.0.0.1 is blocked with 403", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxy.Port()))
		if err != nil {
			t.Fatalf("failed dialing proxy: %v", err)
		}
		defer conn.Close()

		req := "CONNECT 10.0.0.1:443 HTTP/1.1\r\nHost: 10.0.0.1:443\r\n\r\n"
		if _, err := conn.Write([]byte(req)); err != nil {
			t.Fatalf("failed writing request: %v", err)
		}

		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("failed reading response: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("HTTP request to insecure scheme blocked when AllowInsecureHTTP is false", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxy.Port()))
		if err != nil {
			t.Fatalf("failed dialing proxy: %v", err)
		}
		defer conn.Close()

		req := "GET http://127.0.0.1:8080/path HTTP/1.1\r\nHost: 127.0.0.1:8080\r\n\r\n"
		if _, err := conn.Write([]byte(req)); err != nil {
			t.Fatalf("failed writing request: %v", err)
		}

		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("failed reading response: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("Proxy env returns correct local loopback url", func(t *testing.T) {
		env := proxy.Env()
		expectedURL := fmt.Sprintf("http://127.0.0.1:%d", proxy.Port())
		foundHTTP := false
		foundHTTPS := false
		for _, e := range env {
			if e == "HTTP_PROXY="+expectedURL {
				foundHTTP = true
			}
			if e == "HTTPS_PROXY="+expectedURL {
				foundHTTPS = true
			}
		}
		if !foundHTTP || !foundHTTPS {
			t.Errorf("expected proxy environment variables, got %v", env)
		}
	})

	t.Run("CONNECT succeeds to allowed loopback test server", func(t *testing.T) {
		loopbackPolicy := &s3.EndpointSecurityPolicy{
			AllowInsecureHTTP: true,
			PrivateAllowlist:  []string{"127.0.0.1"},
		}
		loopbackProxy, err := StartSecureResticProxy(loopbackPolicy)
		if err != nil {
			t.Fatalf("failed starting loopback proxy: %v", err)
		}
		defer loopbackProxy.Close()

		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok-from-server"))
		}))
		defer ts.Close()

		tsHost := ts.Listener.Addr().String()

		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", loopbackProxy.Port()))
		if err != nil {
			t.Fatalf("failed dialing proxy: %v", err)
		}
		defer conn.Close()

		req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", tsHost, tsHost)
		if _, err := conn.Write([]byte(req)); err != nil {
			t.Fatalf("failed writing request: %v", err)
		}

		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("failed reading response: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 Connection Established, got %d", resp.StatusCode)
		}
	})
}
