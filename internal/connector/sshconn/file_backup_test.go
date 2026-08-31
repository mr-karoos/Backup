package sshconn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

func TestSSHFileBackupCapability_InProcessSSHServer(t *testing.T) {
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate server key: %v", err)
	}
	signer, _ := ssh.NewSignerFromKey(serverKey)
	fp := ssh.FingerprintSHA256(signer.PublicKey())
	user, pass := "testuser", "testpass"

	t.Run("Successful tar stream with excludes (>4 MiB without buffering)", func(t *testing.T) {
		expectedCmd := "tar -C '/var/www/site' -cf - '--exclude=*.log' '--exclude=cache/*' -- ."
		totalBytesToSend := 5 * 1024 * 1024 // 5 MiB
		chunk := []byte(strings.Repeat("T", 64*1024))

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			expectedCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				sent := 0
				for sent < totalBytesToSend {
					toWrite := len(chunk)
					if sent+toWrite > totalBytesToSend {
						toWrite = totalBytesToSend - sent
					}
					_, _ = ch.Write(chunk[:toWrite])
					sent += toWrite
				}
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		_, portStr, _ := net.SplitHostPort(addr)
		port, _ := strconv.Atoi(portStr)

		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
		}
		cred := &payload.PayloadV1{Secret: pass}

		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer
		cfg := connector.FileBackupConfig{
			SourcePath:      "/var/www/site/",
			ExcludePatterns: []string{"*.log", "cache/*"},
		}

		err := capability.BackupFiles(context.Background(), target, cred, cfg, &dest)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if dest.Len() != totalBytesToSend {
			t.Errorf("expected %d bytes, got %d bytes", totalBytesToSend, dest.Len())
		}
	})

	t.Run("Tar utility missing (exit 127) returns ErrArchiveToolMissing", func(t *testing.T) {
		expectedCmd := "tar -C '/var/www/site' -cf - -- ."
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			expectedCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte("bash: tar: command not found\n"))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		_, portStr, _ := net.SplitHostPort(addr)
		port, _ := strconv.Atoi(portStr)

		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
		}
		cred := &payload.PayloadV1{Secret: pass}

		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer
		cfg := connector.FileBackupConfig{
			SourcePath:      "/var/www/site",
			ExcludePatterns: []string{},
		}

		err := capability.BackupFiles(context.Background(), target, cred, cfg, &dest)
		if err != connector.ErrArchiveToolMissing {
			t.Fatalf("expected ErrArchiveToolMissing, got: %v", err)
		}
	})

	t.Run("Tar command non-zero exit code returns ErrArchiveCommandFailed", func(t *testing.T) {
		expectedCmd := "tar -C '/var/www/site' -cf - -- ."
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			expectedCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte("tar: /var/www/site: Cannot open: Permission denied\n"))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 2})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		_, portStr, _ := net.SplitHostPort(addr)
		port, _ := strconv.Atoi(portStr)

		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
		}
		cred := &payload.PayloadV1{Secret: pass}

		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer
		cfg := connector.FileBackupConfig{
			SourcePath:      "/var/www/site",
			ExcludePatterns: []string{},
		}

		err := capability.BackupFiles(context.Background(), target, cred, cfg, &dest)
		if err != connector.ErrArchiveCommandFailed {
			t.Fatalf("expected ErrArchiveCommandFailed, got: %v", err)
		}
	})

	t.Run("Credential payload is zeroed after authentication", func(t *testing.T) {
		expectedCmd := "tar -C '/var/www/site' -cf - -- ."
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			expectedCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte("tar_header_data"))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		_, portStr, _ := net.SplitHostPort(addr)
		port, _ := strconv.Atoi(portStr)

		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
		}
		cred := &payload.PayloadV1{Secret: pass}

		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer
		cfg := connector.FileBackupConfig{
			SourcePath:      "/var/www/site",
			ExcludePatterns: []string{},
		}

		err := capability.BackupFiles(context.Background(), target, cred, cfg, &dest)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cred.Secret != "" || cred.Passphrase != nil {
			t.Errorf("expected cred payload secret to be zeroed/cleared, got Secret: %q", cred.Secret)
		}
	})

	t.Run("Long running file backup is not killed by short connection timeout", func(t *testing.T) {
		shortTimeout := 1
		expectedCmd := "tar -C '/var/www/site' -cf - -- ."
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			expectedCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				for i := 0; i < 6; i++ {
					_, _ = ch.Write([]byte(fmt.Sprintf("chunk-%d\n", i)))
					time.Sleep(200 * time.Millisecond)
				}
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		_, portStr, _ := net.SplitHostPort(addr)
		port, _ := strconv.Atoi(portStr)

		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
			ConnectionTimeout:  &shortTimeout,
		}
		cred := &payload.PayloadV1{Secret: pass}

		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer
		cfg := connector.FileBackupConfig{
			SourcePath:      "/var/www/site",
			ExcludePatterns: []string{},
		}

		err := capability.BackupFiles(context.Background(), target, cred, cfg, &dest)
		if err != nil {
			t.Fatalf("unexpected backup error with cleared TCP deadline: %v", err)
		}

		if !strings.Contains(dest.String(), "chunk-5") {
			t.Errorf("expected complete file stream to finish, got output: %s", dest.String())
		}
	})

	t.Run("Stderr overflow returns ErrArchiveCommandFailed", func(t *testing.T) {
		expectedCmd := "tar -C '/var/www/site' -cf - -- ."
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			expectedCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				// Write >64 KiB to stderr
				hugeStderr := bytes.Repeat([]byte("E"), 70*1024)
				_, _ = ch.Stderr().Write(hugeStderr)
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		_, portStr, _ := net.SplitHostPort(addr)
		port, _ := strconv.Atoi(portStr)

		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
		}
		cred := &payload.PayloadV1{Secret: pass}

		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer
		cfg := connector.FileBackupConfig{
			SourcePath:      "/var/www/site",
			ExcludePatterns: []string{},
		}

		err := capability.BackupFiles(context.Background(), target, cred, cfg, &dest)
		if err != connector.ErrArchiveCommandFailed {
			t.Fatalf("expected ErrArchiveCommandFailed on stderr overflow, got: %v", err)
		}
	})

	t.Run("Invalid source path or exclude pattern returns ErrInvalidFileBackupConfig", func(t *testing.T) {
		target := connector.Target{
			Host:               "127.0.0.1",
			Port:               22,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fp,
		}
		cred := &payload.PayloadV1{Secret: pass}
		capability := NewSSHFileBackupCapability(nil)
		var dest bytes.Buffer

		invalidCases := []connector.FileBackupConfig{
			{SourcePath: "relative/path", ExcludePatterns: []string{}},
			{SourcePath: "/var/www/../etc", ExcludePatterns: []string{}},
			{SourcePath: "/var/www/\x00null", ExcludePatterns: []string{}},
			{SourcePath: "/var/www", ExcludePatterns: []string{""}},
			{SourcePath: "/var/www", ExcludePatterns: []string{"   "}},
			{SourcePath: "/var/www", ExcludePatterns: []string{"\x00bad"}},
			{SourcePath: "/var/www", ExcludePatterns: []string{"*.log\nrm -rf /"}},
		}

		for _, tc := range invalidCases {
			err := capability.BackupFiles(context.Background(), target, cred, tc, &dest)
			if err != connector.ErrInvalidFileBackupConfig {
				t.Errorf("expected ErrInvalidFileBackupConfig for %+v, got: %v", tc, err)
			}
		}
	})
}
