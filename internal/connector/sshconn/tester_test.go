package sshconn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
	"backup-platform/pkg/uuid"

	"golang.org/x/crypto/ssh"
)

func generateTestHostKey(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}
	fp := ssh.FingerprintSHA256(signer.PublicKey())
	return signer, fp
}

func startEphemeralSSHServer(
	t *testing.T,
	serverSigner ssh.Signer,
	expectedPassword string,
	expectedPublicKey ssh.PublicKey,
) (string, func()) {
	t.Helper()
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if expectedPassword != "" && string(password) == expectedPassword {
				return nil, nil
			}
			return nil, errors.New("invalid password")
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if expectedPublicKey != nil && string(key.Marshal()) == string(expectedPublicKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid public key")
		},
		ServerVersion: "SSH-2.0-TestServer_1.0",
	}
	config.AddHostKey(serverSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test listener: %v", err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}

			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				sConn, chans, reqs, err := ssh.NewServerConn(c, config)
				if err != nil {
					return
				}
				defer sConn.Close()
				go ssh.DiscardRequests(reqs)
				for newChannel := range chans {
					_ = newChannel.Reject(ssh.UnknownChannelType, "not supported")
				}
			}(conn)
		}
	}()

	cleanup := func() {
		close(done)
		_ = listener.Close()
		wg.Wait()
	}

	return listener.Addr().String(), cleanup
}

func TestSSHConnectionTester_PasswordAuth_Success(t *testing.T) {
	signer, fp := generateTestHostKey(t)
	addr, cleanup := startEphemeralSSHServer(t, signer, "valid-password", nil)
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port := 22
	if p, err := net.LookupPort("tcp", portStr); err == nil {
		port = p
	}

	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHPassword,
		Username:           "testuser",
		HostKeyFingerprint: &fp,
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "valid-password",
	}

	result, err := tester.TestConnection(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected probe success, got failure: kind=%s, reason=%s", result.FailureKind, result.SafeReason)
	}
	if result.Details["server_banner"] != "SSH-2.0-TestServer_1.0" {
		t.Errorf("unexpected banner: %v", result.Details["server_banner"])
	}
	if result.Details["auth_method"] != "password" {
		t.Errorf("unexpected auth_method: %v", result.Details["auth_method"])
	}
}

func TestSSHConnectionTester_PasswordAuth_WrongPassword(t *testing.T) {
	signer, fp := generateTestHostKey(t)
	addr, cleanup := startEphemeralSSHServer(t, signer, "valid-password", nil)
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := net.LookupPort("tcp", portStr)

	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHPassword,
		Username:           "testuser",
		HostKeyFingerprint: &fp,
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "wrong-password",
	}

	result, err := tester.TestConnection(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected probe failure for wrong password")
	}
	if result.FailureKind != connector.FailureKindAuthFailed {
		t.Errorf("expected FailureKindAuthFailed, got: %s", result.FailureKind)
	}
	if result.SafeReason != "authentication failed" {
		t.Errorf("expected safe reason 'authentication failed', got: %s", result.SafeReason)
	}
}

func TestSSHConnectionTester_PublicKeyAuth_Success(t *testing.T) {
	signer, fp := generateTestHostKey(t)

	// Generate Client Key
	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatalf("failed to create client signer: %v", err)
	}

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(clientPriv)
	if err != nil {
		t.Fatalf("failed to marshal pkcs8: %v", err)
	}
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	}
	clientPrivPEM := string(pem.EncodeToMemory(pemBlock))

	addr, cleanup := startEphemeralSSHServer(t, signer, "", clientSigner.PublicKey())
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := net.LookupPort("tcp", portStr)

	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHKey,
		Username:           "testuser",
		HostKeyFingerprint: &fp,
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  clientPrivPEM,
	}

	result, err := tester.TestConnection(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected probe success, got failure: kind=%s, reason=%s", result.FailureKind, result.SafeReason)
	}
	if result.Details["auth_method"] != "publickey" {
		t.Errorf("unexpected auth_method: %v", result.Details["auth_method"])
	}
}

func TestSSHConnectionTester_EncryptedPrivateKey_WithPassphrase(t *testing.T) {
	signer, fp := generateTestHostKey(t)

	// Create encrypted client private key (PKCS#1 RSA)
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	clientSigner, err := ssh.NewSignerFromKey(rsaPriv)
	if err != nil {
		t.Fatalf("failed to create RSA signer: %v", err)
	}
	pkcs1Bytes := x509.MarshalPKCS1PrivateKey(rsaPriv)

	passphrase := "my-ssh-passphrase"
	// Encrypt PKCS#1 block with AES-256-CBC PEM encryption
	//nolint:staticcheck // testing encrypted PEM passphrase decryption support
	encryptedBlock, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", pkcs1Bytes, []byte(passphrase), x509.PEMCipherAES256)
	if err != nil {
		t.Fatalf("failed to encrypt PEM: %v", err)
	}
	encryptedPEM := string(pem.EncodeToMemory(encryptedBlock))

	addr, cleanup := startEphemeralSSHServer(t, signer, "", clientSigner.PublicKey())
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := net.LookupPort("tcp", portStr)

	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHKey,
		Username:           "testuser",
		HostKeyFingerprint: &fp,
	}

	t.Run("Correct passphrase authenticates successfully", func(t *testing.T) {
		credPayload := &payload.PayloadV1{
			Version:    1,
			Secret:     encryptedPEM,
			Passphrase: &passphrase,
		}

		result, err := tester.TestConnection(context.Background(), target, credPayload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Fatalf("expected success with valid passphrase, got: %s", result.SafeReason)
		}
	})

	t.Run("Wrong passphrase results in internal parse failure", func(t *testing.T) {
		wrongPass := "wrong-passphrase"
		credPayload := &payload.PayloadV1{
			Version:    1,
			Secret:     encryptedPEM,
			Passphrase: &wrongPass,
		}

		_, err := tester.TestConnection(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected internal parse error for wrong passphrase")
		}
	})
}

func TestSSHConnectionTester_HostKeyMismatch(t *testing.T) {
	signer, _ := generateTestHostKey(t)
	wrongFP := "SHA256:0000000000000000000000000000000000000000000"

	addr, cleanup := startEphemeralSSHServer(t, signer, "valid-password", nil)
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := net.LookupPort("tcp", portStr)

	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHPassword,
		Username:           "testuser",
		HostKeyFingerprint: &wrongFP,
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "valid-password",
	}

	result, err := tester.TestConnection(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected probe failure for host key mismatch")
	}
	if result.FailureKind != connector.FailureKindHostKeyMismatch {
		t.Errorf("expected FailureKindHostKeyMismatch, got: %s", result.FailureKind)
	}
	if result.SafeReason != "SSH host key verification failed" {
		t.Errorf("expected safe reason 'SSH host key verification failed', got: %s", result.SafeReason)
	}
}

func TestSSHConnectionTester_MissingFingerprint_PreflightRejection(t *testing.T) {
	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               "127.0.0.1",
		Port:               22,
		AuthType:           resDomain.AuthTypeSSHPassword,
		Username:           "testuser",
		HostKeyFingerprint: nil,
	}

	credPayload := &payload.PayloadV1{
		Version: 1,
		Secret:  "valid-password",
	}

	_, err := tester.TestConnection(context.Background(), target, credPayload)
	if !errors.Is(err, resDomain.ErrInvalidHostKeyFingerprint) {
		t.Errorf("expected ErrInvalidHostKeyFingerprint, got: %v", err)
	}
}

func TestSSHConnectionTester_HandshakeTimeout(t *testing.T) {
	// Start TCP listener that accepts connection but never writes SSH handshake
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold connection open without sending SSH banner
			defer conn.Close()
		}
	}()

	host, portStr, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := net.LookupPort("tcp", portStr)

	timeoutSec := 1
	fp := "SHA256:dummy"
	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHPassword,
		Username:           "testuser",
		HostKeyFingerprint: &fp,
		ConnectionTimeout:  &timeoutSec,
	}

	result, err := tester.TestConnection(context.Background(), target, &payload.PayloadV1{Version: 1, Secret: "pass"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Fatalf("expected timeout failure")
	}
	if result.FailureKind != connector.FailureKindTimeout {
		t.Errorf("expected FailureKindTimeout, got: %s", result.FailureKind)
	}
	if result.SafeReason != "connection timed out" {
		t.Errorf("expected 'connection timed out', got: %s", result.SafeReason)
	}
}

func TestSSHConnectionTester_CallerCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	host, portStr, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := net.LookupPort("tcp", portStr)

	fp := "SHA256:dummy"
	tester := NewSSHConnectionTester(nil)
	target := connector.Target{
		ResourceID:         uuid.New(),
		OrganizationID:     uuid.New(),
		ResourceType:       resDomain.TypeUbuntuSSH,
		Host:               host,
		Port:               port,
		AuthType:           resDomain.AuthTypeSSHPassword,
		Username:           "testuser",
		HostKeyFingerprint: &fp,
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = tester.TestConnection(parentCtx, target, &payload.PayloadV1{Version: 1, Secret: "pass"})
	if err == nil {
		t.Fatalf("expected error when caller parent context is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestSanitizeBanner(t *testing.T) {
	t.Run("strips control characters and whitespace", func(t *testing.T) {
		raw := "  \x00\x1b[31mSSH-2.0-OpenSSH_8.9p1\r\n Ubuntu-3ubuntu0.1\x7f  "
		clean := SanitizeBanner(raw)
		if strings.Contains(clean, "\x00") || strings.Contains(clean, "\x1b") || strings.Contains(clean, "\r") || strings.Contains(clean, "\n") {
			t.Errorf("failed to strip control characters: %q", clean)
		}
	})

	t.Run("truncates ASCII to 255 runes", func(t *testing.T) {
		raw := strings.Repeat("A", 300)
		clean := SanitizeBanner(raw)
		if len(clean) != 255 {
			t.Errorf("expected length 255, got: %d", len(clean))
		}
	})

	t.Run("truncates multibyte Unicode characters safely without corrupting UTF-8", func(t *testing.T) {
		// 300 Persian 2-byte/3-byte runes: "سرور لینوکس ابری "
		raw := strings.Repeat("سرور ابری ", 30) // ~300 runes
		clean := SanitizeBanner(raw)

		runeCount := utf8.RuneCountInString(clean)
		if runeCount > 255 {
			t.Errorf("expected <= 255 runes, got %d", runeCount)
		}
		if !utf8.ValidString(clean) {
			t.Errorf("corrupted UTF-8 detected after truncation")
		}
	})
}
