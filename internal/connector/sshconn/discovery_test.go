package sshconn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"

	"golang.org/x/crypto/ssh"
)

func TestParseMySQLTSVOutput(t *testing.T) {
	t.Run("Parses valid TSV with ASCII and Unicode database names", func(t *testing.T) {
		name1 := "ecommerce_prod"
		hex1 := hex.EncodeToString([]byte(name1))

		name2 := "پایگاه_داده_تست"
		hex2 := hex.EncodeToString([]byte(name2))

		tsv := fmt.Sprintf("%s\t104857600\t48\n%s\t2048\t5\n", hex1, hex2)

		dbs, err := parseMySQLTSVOutput([]byte(tsv))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(dbs) != 2 {
			t.Fatalf("expected 2 databases, got %d", len(dbs))
		}

		if dbs[0].Name != name1 || dbs[0].SizeBytes != 104857600 || *dbs[0].TablesCount != 48 {
			t.Errorf("unexpected database 0: %+v", dbs[0])
		}
		if dbs[1].Name != name2 || dbs[1].SizeBytes != 2048 || *dbs[1].TablesCount != 5 {
			t.Errorf("unexpected database 1: %+v", dbs[1])
		}
	})

	t.Run("Parses valid database with zero tables and zero size", func(t *testing.T) {
		name := "empty_db"
		hexName := hex.EncodeToString([]byte(name))
		tsv := fmt.Sprintf("%s\t0\t0\n", hexName)

		dbs, err := parseMySQLTSVOutput([]byte(tsv))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dbs) != 1 {
			t.Fatalf("expected 1 database, got %d", len(dbs))
		}
		if dbs[0].SizeBytes != 0 || *dbs[0].TablesCount != 0 {
			t.Errorf("unexpected database values: %+v", dbs[0])
		}
	})

	t.Run("Empty TSV returns empty slice with nil error", func(t *testing.T) {
		dbs, err := parseMySQLTSVOutput([]byte(""))
		if err != nil {
			t.Fatalf("unexpected error on empty output: %v", err)
		}
		if len(dbs) != 0 {
			t.Errorf("expected 0 databases, got %d", len(dbs))
		}
	})

	t.Run("Rejects invalid column count", func(t *testing.T) {
		invalidTSVs := []string{
			"65636F6D6D657263655F70726F64\t104857600\n",
			"65636F6D6D657263655F70726F64\t104857600\t48\textra_column\n",
			"single_column\n",
		}
		for _, tsv := range invalidTSVs {
			_, err := parseMySQLTSVOutput([]byte(tsv))
			if err == nil {
				t.Errorf("expected error for invalid column count in %q", tsv)
			}
		}
	})

	t.Run("Rejects invalid hex database name", func(t *testing.T) {
		tsv := "NOT_VALID_HEX_ZZZ\t104857600\t48\n"
		_, err := parseMySQLTSVOutput([]byte(tsv))
		if err == nil {
			t.Fatalf("expected error for invalid hex name")
		}
	})

	t.Run("Rejects negative numeric values", func(t *testing.T) {
		hexName := hex.EncodeToString([]byte("test_db"))
		_, err := parseMySQLTSVOutput([]byte(fmt.Sprintf("%s\t-100\t5\n", hexName)))
		if err == nil {
			t.Errorf("expected error for negative size")
		}

		_, err = parseMySQLTSVOutput([]byte(fmt.Sprintf("%s\t100\t-5\n", hexName)))
		if err == nil {
			t.Errorf("expected error for negative tables count")
		}
	})

	t.Run("Rejects non-integer numbers and overflow", func(t *testing.T) {
		hexName := hex.EncodeToString([]byte("test_db"))
		_, err := parseMySQLTSVOutput([]byte(fmt.Sprintf("%s\t100.5\t5\n", hexName)))
		if err == nil {
			t.Errorf("expected error for float size")
		}

		_, err = parseMySQLTSVOutput([]byte(fmt.Sprintf("%s\t99999999999999999999999999999999999\t5\n", hexName)))
		if err == nil {
			t.Errorf("expected error for integer overflow")
		}
	})
}

func TestStaticCommandSecurity(t *testing.T) {
	// Assert that static discovery commands contain zero credential keywords or user parameters
	if strings.Contains(mysqlDiscoveryCmd, "PASSWORD") || strings.Contains(mysqlDiscoveryCmd, "--password") {
		t.Errorf("SECURITY FLAW: mysql command contains password flag")
	}
	if strings.Contains(mariadbDiscoveryCmd, "PASSWORD") || strings.Contains(mariadbDiscoveryCmd, "--password") {
		t.Errorf("SECURITY FLAW: mariadb command contains password flag")
	}
	if strings.Contains(mysqlDiscoveryCmd, "sudo") || strings.Contains(mariadbDiscoveryCmd, "sudo") {
		t.Errorf("SECURITY FLAW: discovery command should not use interactive sudo")
	}
}

// In-process mock SSH server helper
func startMockSSHDiscoveryServer(
	t *testing.T,
	serverKey *rsa.PrivateKey,
	validUser, validPass string,
	commandHandlers map[string]func(session ssh.Channel, req *ssh.Request),
) (string, func()) {
	t.Helper()

	signer, err := ssh.NewSignerFromKey(serverKey)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == validUser && string(pass) == validPass {
				return nil, nil
			}
			return nil, errors.New("auth failed")
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	_, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			tcpConn, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				sshConn, chans, reqs, err := ssh.NewServerConn(c, config)
				if err != nil {
					return
				}
				defer sshConn.Close()

				go ssh.DiscardRequests(reqs)

				for newChannel := range chans {
					if newChannel.ChannelType() != "session" {
						_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
						continue
					}

					channel, requests, err := newChannel.Accept()
					if err != nil {
						return
					}

					go func(ch ssh.Channel, in <-chan *ssh.Request) {
						defer ch.Close()
						for req := range in {
							if req.Type == "exec" {
								// Format: 4-byte length + command string
								if len(req.Payload) >= 4 {
									cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
									if len(req.Payload) >= 4+cmdLen {
										cmd := string(req.Payload[4 : 4+cmdLen])
										if handler, ok := commandHandlers[cmd]; ok {
											handler(ch, req)
										} else {
											// Default: command not found (exit status 127)
											_ = req.Reply(true, nil)
											_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
										}
									}
								}
								return
							}
							_ = req.Reply(false, nil)
						}
					}(channel, requests)
				}
			}(tcpConn)
		}
	}()

	cleanup := func() {
		cancel()
		_ = listener.Close()
	}

	return listener.Addr().String(), cleanup
}

func TestSSHDatabaseDiscoverer_InProcessSSHServer(t *testing.T) {
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate server key: %v", err)
	}
	signer, _ := ssh.NewSignerFromKey(serverKey)
	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())

	user := "ubuntu"
	pass := "secretpassword"

	t.Run("Successful discovery via mysql CLI", func(t *testing.T) {
		name := "ecommerce_prod"
		hexName := hex.EncodeToString([]byte(name))
		tsvOutput := fmt.Sprintf("%s\t104857600\t48\n", hexName)

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			mysqlDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte(tsvOutput))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		host, portStr, _ := net.SplitHostPort(addr)
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		timeout := 5
		target := connector.Target{
			Host:               host,
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			ConnectionTimeout:  &timeout,
		}
		credPayload := &payload.PayloadV1{Secret: pass}

		discoverer := NewSSHDatabaseDiscoverer(nil)
		dbs, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err != nil {
			t.Fatalf("unexpected discovery error: %v", err)
		}

		if len(dbs) != 1 {
			t.Fatalf("expected 1 database, got %d", len(dbs))
		}
		if dbs[0].Name != "ecommerce_prod" || dbs[0].SizeBytes != 104857600 || *dbs[0].TablesCount != 48 {
			t.Errorf("unexpected database content: %+v", dbs[0])
		}
	})

	t.Run("Fallback to mariadb CLI when mysql returns 127", func(t *testing.T) {
		name := "mariadb_app"
		hexName := hex.EncodeToString([]byte(name))
		tsvOutput := fmt.Sprintf("%s\t52428800\t20\n", hexName)

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			mysqlDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				// Exit 127 for mysql
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
			},
			mariadbDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Write([]byte(tsvOutput))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		host, portStr, _ := net.SplitHostPort(addr)
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		timeout := 5
		target := connector.Target{
			Host:               host,
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			ConnectionTimeout:  &timeout,
		}
		credPayload := &payload.PayloadV1{Secret: pass}

		discoverer := NewSSHDatabaseDiscoverer(nil)
		dbs, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err != nil {
			t.Fatalf("unexpected discovery error: %v", err)
		}

		if len(dbs) != 1 {
			t.Fatalf("expected 1 database, got %d", len(dbs))
		}
		if dbs[0].Name != "mariadb_app" || dbs[0].SizeBytes != 52428800 || *dbs[0].TablesCount != 20 {
			t.Errorf("unexpected database content: %+v", dbs[0])
		}
	})

	t.Run("Neither mysql nor mariadb available returns error", func(t *testing.T) {
		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			mysqlDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
			},
			mariadbDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 127})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		host, portStr, _ := net.SplitHostPort(addr)
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		timeout := 5
		target := connector.Target{
			Host:               host,
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			ConnectionTimeout:  &timeout,
		}
		credPayload := &payload.PayloadV1{Secret: pass}

		discoverer := NewSSHDatabaseDiscoverer(nil)
		_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected error when neither CLI is installed")
		}
	})

	t.Run("Stderr containing sensitive credentials is not leaked in error", func(t *testing.T) {
		secretLeak := "Access denied password=SUPERSECRET_SSH_LEAK /var/lib/mysql"

		handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
			mysqlDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte(secretLeak))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
			},
			mariadbDiscoveryCmd: func(ch ssh.Channel, req *ssh.Request) {
				_ = req.Reply(true, nil)
				_, _ = ch.Stderr().Write([]byte(secretLeak))
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
			},
		}

		addr, cleanup := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
		defer cleanup()

		host, portStr, _ := net.SplitHostPort(addr)
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		timeout := 5
		target := connector.Target{
			Host:               host,
			Port:               port,
			Username:           user,
			AuthType:           resDomain.AuthTypeSSHPassword,
			HostKeyFingerprint: &fingerprint,
			ConnectionTimeout:  &timeout,
		}
		credPayload := &payload.PayloadV1{Secret: pass}

		discoverer := NewSSHDatabaseDiscoverer(nil)
		_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected error on command failure")
		}

		if strings.Contains(err.Error(), "SUPERSECRET_SSH_LEAK") || strings.Contains(err.Error(), "/var/lib/mysql") {
			t.Errorf("SECURITY FLAW: sensitive stderr leaked in discovery error: %v", err)
		}
	})
}

func connectTestSSHClient(t *testing.T, addr, user, pass string, serverKey *rsa.PrivateKey) (*ssh.Client, func()) {
	t.Helper()
	signer, _ := ssh.NewSignerFromKey(serverKey)
	expectedFingerprint := ssh.FingerprintSHA256(signer.PublicKey())

	conf := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != expectedFingerprint {
				return errors.New("host key mismatch")
			}
			return nil
		},
		Timeout: 3 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		t.Fatalf("failed to dial mock ssh server: %v", err)
	}

	return client, func() {
		_ = client.Close()
	}
}

func TestRunBoundedRemoteCommand_ConcurrentStdoutStderr(t *testing.T) {
	serverKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	user, pass := "testuser", "testpass"

	stdoutData := strings.Repeat("stdout line data\n", 500) // ~8.5 KiB
	stderrData := strings.Repeat("stderr log data\n", 300)  // ~4.8 KiB

	cmdName := "echo_streams"
	handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		cmdName: func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)

			// Concurrently write both streams
			go func() {
				_, _ = ch.Stderr().Write([]byte(stderrData))
			}()
			_, _ = ch.Write([]byte(stdoutData))
			_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		},
	}

	addr, cleanupServer := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
	defer cleanupServer()

	client, cleanupClient := connectTestSSHClient(t, addr, user, pass, serverKey)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := runBoundedRemoteCommandWithLimits(ctx, client, cmdName, 64*1024, 64*1024)
	if err != nil {
		t.Fatalf("unexpected command error: %v", err)
	}

	if string(output) != stdoutData {
		t.Errorf("stdout data mismatch: expected %d bytes, got %d", len(stdoutData), len(output))
	}
}

func TestRunBoundedRemoteCommand_StdoutOverflow(t *testing.T) {
	serverKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	user, pass := "testuser", "testpass"

	cmdName := "overflow_stdout"
	handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		cmdName: func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)
			// Write 2000 bytes into stdout
			_, _ = ch.Write([]byte(strings.Repeat("A", 2000)))
			_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		},
	}

	addr, cleanupServer := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
	defer cleanupServer()

	client, cleanupClient := connectTestSSHClient(t, addr, user, pass, serverKey)
	defer cleanupClient()

	// Short timeout ensures we do not hang or wait full timeout on overflow
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := runBoundedRemoteCommandWithLimits(ctx, client, cmdName, 500, 64*1024)
	if err == nil {
		t.Fatalf("expected error on stdout overflow, got nil")
	}

	if !strings.Contains(err.Error(), "stdout exceeded maximum allowed limit") {
		t.Errorf("expected stdout overflow error message, got: %v", err)
	}
}

func TestRunBoundedRemoteCommand_StderrOverflow(t *testing.T) {
	serverKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	user, pass := "testuser", "testpass"

	cmdName := "overflow_stderr"
	handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		cmdName: func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)
			// Write 2000 bytes into stderr
			_, _ = ch.Stderr().Write([]byte(strings.Repeat("E", 2000)))
			_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		},
	}

	addr, cleanupServer := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
	defer cleanupServer()

	client, cleanupClient := connectTestSSHClient(t, addr, user, pass, serverKey)
	defer cleanupClient()

	// Short timeout ensures we do not hang or wait full timeout on overflow
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := runBoundedRemoteCommandWithLimits(ctx, client, cmdName, 64*1024, 500)
	if err == nil {
		t.Fatalf("expected error on stderr overflow, got nil")
	}

	if !strings.Contains(err.Error(), "stderr exceeded maximum allowed limit") {
		t.Errorf("expected stderr overflow error message, got: %v", err)
	}
}

func TestRunBoundedRemoteCommand_ContextCancellation(t *testing.T) {
	serverKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	user, pass := "testuser", "testpass"

	cmdName := "hang_command"
	handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		cmdName: func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)
			// Send partial data and block to simulate slow/hanging command
			_, _ = ch.Write([]byte("partial data\n"))
			time.Sleep(5 * time.Second)
		},
	}

	addr, cleanupServer := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
	defer cleanupServer()

	client, cleanupClient := connectTestSSHClient(t, addr, user, pass, serverKey)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runBoundedRemoteCommandWithLimits(ctx, client, cmdName, 64*1024, 64*1024)
	duration := time.Since(start)

	if err == nil {
		t.Fatalf("expected error on context cancellation, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation error, got: %v", err)
	}
	if duration > 1*time.Second {
		t.Errorf("cancellation took too long (%v), expected prompt return", duration)
	}
}

func TestRunBoundedRemoteCommand_DeadlockRegression(t *testing.T) {
	serverKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	user, pass := "testuser", "testpass"

	cmdName := "interleaved_bulk"
	stdoutPayload := strings.Repeat("O", 30*1024)
	stderrPayload := strings.Repeat("E", 30*1024)

	handlers := map[string]func(ch ssh.Channel, req *ssh.Request){
		cmdName: func(ch ssh.Channel, req *ssh.Request) {
			_ = req.Reply(true, nil)

			// Concurrently pump both streams
			go func() {
				_, _ = ch.Stderr().Write([]byte(stderrPayload))
			}()
			_, _ = ch.Write([]byte(stdoutPayload))
			_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
		},
	}

	addr, cleanupServer := startMockSSHDiscoveryServer(t, serverKey, user, pass, handlers)
	defer cleanupServer()

	client, cleanupClient := connectTestSSHClient(t, addr, user, pass, serverKey)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := runBoundedRemoteCommandWithLimits(ctx, client, cmdName, 64*1024, 64*1024)
	if err != nil {
		t.Fatalf("deadlock regression or unexpected error: %v", err)
	}

	if len(output) != len(stdoutPayload) {
		t.Errorf("expected %d bytes stdout, got %d", len(stdoutPayload), len(output))
	}
}
