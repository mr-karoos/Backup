package sshconn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
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

func TestPOSIXShellQuote(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "simple alphanumeric",
			input:    "normal_db",
			expected: "'normal_db'",
		},
		{
			name:     "contains spaces",
			input:    "db name",
			expected: "'db name'",
		},
		{
			name:     "contains dashes",
			input:    "db-name",
			expected: "'db-name'",
		},
		{
			name:     "semicolon command injection attempt",
			input:    "db;touch_x",
			expected: "'db;touch_x'",
		},
		{
			name:     "subshell command injection attempt",
			input:    "db$(touch_x)",
			expected: "'db$(touch_x)'",
		},
		{
			name:     "backtick command injection attempt",
			input:    "db`touch_x`",
			expected: "'db`touch_x`'",
		},
		{
			name:     "single quote in name",
			input:    "db'name",
			expected: `'db'\''name'`,
		},
		{
			name:     "cli flag injection attempt",
			input:    "--defaults-file=/tmp/x",
			expected: "'--defaults-file=/tmp/x'",
		},
		{
			name:     "unicode database name",
			input:    "دیتابیس_فروشگاه",
			expected: "'دیتابیس_فروشگاه'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := POSIXShellQuote(tc.input)
			if got != tc.expected {
				t.Errorf("POSIXShellQuote(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestSSHDatabaseBackupCapability_InProcessSSHServer(t *testing.T) {
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate server key: %v", err)
	}
	signer, _ := ssh.NewSignerFromKey(serverKey)
	fp := ssh.FingerprintSHA256(signer.PublicKey())
	user, pass := "testuser", "testpass"

	t.Run("Successful backup stream via mysqldump (>4 MiB without buffering)", func(t *testing.T) {
		mockDumpHeader := "-- MySQL dump 10.13\nCREATE DATABASE `prod_db`;\nUSE `prod_db`;\n"
		totalBytesToSend := 5 * 1024 * 1024 // 5 MiB (verifies backup is not bounded by discovery's 4 MiB limit)
		chunk := []byte(strings.Repeat("A", 64*1024))

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte(mockDumpHeader))
				sent := len(mockDumpHeader)
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err != nil {
			t.Fatalf("unexpected backup error: %v", err)
		}

		if dest.Len() != totalBytesToSend {
			t.Errorf("expected %d bytes streamed, got %d", totalBytesToSend, dest.Len())
		}
		if !strings.HasPrefix(dest.String(), mockDumpHeader) {
			t.Errorf("expected dump to start with mock header")
		}
	})

	t.Run("Fallback to mariadb-dump when mysqldump returns 127 with 0 bytes written", func(t *testing.T) {
		mockDump := "-- MariaDB dump 10.19\nCREATE DATABASE `prod_db`;\n"

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				// Exit 127 without writing any stdout
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
			},
			"mariadb-dump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte(mockDump))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err != nil {
			t.Fatalf("unexpected backup error on fallback: %v", err)
		}

		if dest.String() != mockDump {
			t.Errorf("expected fallback dump content %q, got %q", mockDump, dest.String())
		}
	})

	t.Run("No fallback if mysqldump wrote partial stdout before exiting 127", func(t *testing.T) {
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte("partial corrupted dump"))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
			},
			"mariadb-dump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte("mariadb output"))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err == nil {
			t.Fatalf("expected error when mysqldump fails after partial output")
		}

		if strings.Contains(dest.String(), "mariadb output") {
			t.Errorf("mariadb-dump should NOT have been invoked after partial stdout from mysqldump")
		}
	})

	t.Run("Concurrent stdout dump and stderr noise without deadlock", func(t *testing.T) {
		mockDump := "-- MySQL dump 10.13\n"
		stderrNoise := strings.Repeat("warning: deprecated option\n", 100)

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte(stderrNoise))
				_, _ = ch.Write([]byte(mockDump))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err != nil {
			t.Fatalf("unexpected backup error: %v", err)
		}

		if dest.String() != mockDump {
			t.Errorf("expected dump %q, got %q", mockDump, dest.String())
		}
	})

	t.Run("Stderr overflow (>64 KiB) triggers failure and closes session", func(t *testing.T) {
		largeStderr := strings.Repeat("E", 70*1024) // 70 KiB > 64 KiB limit

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte(largeStderr))
				_, _ = ch.Write([]byte("-- MySQL dump 10.13\n"))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err == nil {
			t.Fatalf("expected error on stderr overflow")
		}
	})

	t.Run("Downstream destination writer failure terminates remote session promptly", func(t *testing.T) {
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				for i := 0; i < 100; i++ {
					if _, err := ch.Write([]byte(strings.Repeat("D", 1024))); err != nil {
						return // Session terminated
					}
					time.Sleep(10 * time.Millisecond)
				}
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

		capability := NewSSHDatabaseBackupCapability(nil)
		fw := &failingWriter{failAfterBytes: 512}
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", fw)
		if err == nil {
			t.Fatalf("expected error when destination writer fails")
		}
	})

	t.Run("Stderr containing sensitive credentials is not leaked in error", func(t *testing.T) {
		sensitiveStderr := "Access denied for user 'dbuser'@'localhost' (using password: YES) password=SUPER_SECRET_MYSQL_PASS /var/lib/mysql"

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte(sensitiveStderr))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err == nil {
			t.Fatalf("expected error on mysqldump failure")
		}

		errMsg := err.Error()
		if strings.Contains(errMsg, "SUPER_SECRET_MYSQL_PASS") || strings.Contains(errMsg, "/var/lib/mysql") {
			t.Errorf("SECURITY FLAW: sensitive credentials leaked in error message: %s", errMsg)
		}
	})

	t.Run("Payload Secret is cleared immediately after SSH authentication", func(t *testing.T) {
		mockDump := "-- MySQL dump\n"
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte(mockDump))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cred.Secret != "" || cred.Passphrase != nil {
			t.Errorf("expected cred payload secret to be zeroed/cleared, got Secret: %q", cred.Secret)
		}
	})

	t.Run("Long running backup dump is not killed by short connection timeout", func(t *testing.T) {
		shortTimeout := 1 // 1 second connection timeout
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			"mysqldump --single-transaction --quick --routines --triggers --hex-blob -- 'prod_db'": func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				// Stream slowly beyond the connection timeout (e.g. 1.2 seconds)
				for i := 0; i < 6; i++ {
					_, _ = ch.Write([]byte(fmt.Sprintf("-- chunk %d\n", i)))
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

		capability := NewSSHDatabaseBackupCapability(nil)
		var dest bytes.Buffer
		err := capability.BackupDatabase(context.Background(), target, cred, "prod_db", &dest)
		if err != nil {
			t.Fatalf("unexpected backup error with cleared TCP deadline: %v", err)
		}

		if !strings.Contains(dest.String(), "-- chunk 5") {
			t.Errorf("expected complete dump to finish, got output: %s", dest.String())
		}
	})
}

type failingWriter struct {
	written        int
	failAfterBytes int
}

func (fw *failingWriter) Write(p []byte) (int, error) {
	if fw.written >= fw.failAfterBytes {
		return 0, errors.New("disk full / write error")
	}
	remaining := fw.failAfterBytes - fw.written
	if len(p) > remaining {
		fw.written += remaining
		return remaining, errors.New("disk full / write error")
	}
	fw.written += len(p)
	return len(p), nil
}
