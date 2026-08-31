package domain

import (
	"errors"
	"testing"

	credDomain "backup-platform/internal/credential/domain"
)

func TestValidateConnectorHost(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		// Valid cases
		{name: "valid IPv4", input: "192.168.1.1", expected: "192.168.1.1", expectError: false},
		{name: "valid IPv6 unbracketed", input: "2001:db8::1", expected: "2001:db8::1", expectError: false},
		{name: "valid IPv6 bracketed canonicalizes to unbracketed", input: "[2001:db8::1]", expected: "2001:db8::1", expectError: false},
		{name: "valid DNS FQDN", input: "server1.example.com", expected: "server1.example.com", expectError: false},
		{name: "uppercase DNS normalizes to lowercase", input: "CPANEL.Example.COM", expected: "cpanel.example.com", expectError: false},
		{name: "valid single-label hostname", input: "myserver", expected: "myserver", expectError: false},
		{name: "valid hostname with hyphen", input: "web-server-01.prod.internal", expected: "web-server-01.prod.internal", expectError: false},
		{name: "trimmed whitespace", input: "  10.0.0.1  ", expected: "10.0.0.1", expectError: false},

		// Invalid cases
		{name: "empty string", input: "", expectError: true},
		{name: "whitespace only", input: "   ", expectError: true},
		{name: "space in hostname", input: "foo bar", expectError: true},
		{name: "email address syntax", input: "abc@example.com", expectError: true},
		{name: "question mark in host", input: "example?.com", expectError: true},
		{name: "hash in host", input: "example#.com", expectError: true},
		{name: "underscore in hostname", input: "foo_bar.example.com", expectError: true},
		{name: "double dot in hostname", input: "example..com", expectError: true},
		{name: "leading hyphen in label", input: "-example.com", expectError: true},
		{name: "trailing hyphen in label", input: "example-.com", expectError: true},
		{name: "leading dot", input: ".example.com", expectError: true},
		{name: "trailing dot", input: "example.com.", expectError: true},
		{name: "host with port in DNS", input: "example.com:22", expectError: true},
		{name: "host with port in IPv4", input: "192.168.1.10:22", expectError: true},
		{name: "host with port in bracketed IPv6", input: "[2001:db8::1]:22", expectError: true},
		{name: "URL with HTTP scheme", input: "http://example.com", expectError: true},
		{name: "URL with HTTPS scheme", input: "https://example.com/api", expectError: true},
		{name: "host with path", input: "example.com/cpanel", expectError: true},
		{name: "host with backslash", input: "example.com\\test", expectError: true},
		{name: "label exceeding 63 characters", input: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz.com", expectError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateConnectorHost(tc.input)
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error for input %q, got success with output %q", tc.input, got)
				}
				if !errors.Is(err, ErrInvalidConnectorHost) {
					t.Errorf("expected ErrInvalidConnectorHost, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tc.input, err)
				}
				if got != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, got)
				}
			}
		})
	}
}

func TestValidateConnectorConfig(t *testing.T) {
	trueVal := true
	falseVal := false
	validTimeout := 30
	invalidTimeoutHigh := 301
	invalidTimeoutZero := 0

	t.Run("Ubuntu SSH rejects use_https whether true or false", func(t *testing.T) {
		_, err := ValidateConnectorConfig(TypeUbuntuSSH, "root", &validTimeout, &trueVal)
		if !errors.Is(err, ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig for Ubuntu with use_https=true, got: %v", err)
		}

		_, err = ValidateConnectorConfig(TypeUbuntuSSH, "root", &validTimeout, &falseVal)
		if !errors.Is(err, ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig for Ubuntu with use_https=false, got: %v", err)
		}
	})

	t.Run("Ubuntu SSH accepts use_https == nil", func(t *testing.T) {
		cfg, err := ValidateConnectorConfig(TypeUbuntuSSH, "root", &validTimeout, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Username != "root" || *cfg.ConnectionTimeoutSeconds != 30 || cfg.UseHTTPS != nil {
			t.Errorf("unexpected config result: %+v", cfg)
		}
	})

	t.Run("cPanel accepts use_https == true, false, or nil", func(t *testing.T) {
		cfg1, err := ValidateConnectorConfig(TypeCPanel, "cpuser", &validTimeout, &trueVal)
		if err != nil || cfg1.UseHTTPS == nil || !*cfg1.UseHTTPS {
			t.Errorf("expected cPanel with use_https=true, got err: %v, cfg: %+v", err, cfg1)
		}

		cfg2, err := ValidateConnectorConfig(TypeCPanel, "cpuser", &validTimeout, &falseVal)
		if err != nil || cfg2.UseHTTPS == nil || *cfg2.UseHTTPS {
			t.Errorf("expected cPanel with use_https=false, got err: %v, cfg: %+v", err, cfg2)
		}

		cfg3, err := ValidateConnectorConfig(TypeCPanel, "cpuser", &validTimeout, nil)
		if err != nil || cfg3.UseHTTPS != nil {
			t.Errorf("expected cPanel with use_https=nil, got err: %v, cfg: %+v", err, cfg3)
		}
	})

	t.Run("Rejects invalid username", func(t *testing.T) {
		_, err := ValidateConnectorConfig(TypeUbuntuSSH, "", &validTimeout, nil)
		if !errors.Is(err, ErrInvalidConnectorUsername) {
			t.Errorf("expected ErrInvalidConnectorUsername, got: %v", err)
		}
	})

	t.Run("Rejects invalid timeout", func(t *testing.T) {
		_, err := ValidateConnectorConfig(TypeUbuntuSSH, "root", &invalidTimeoutHigh, nil)
		if !errors.Is(err, ErrInvalidConnectionTimeout) {
			t.Errorf("expected ErrInvalidConnectionTimeout, got: %v", err)
		}

		_, err = ValidateConnectorConfig(TypeUbuntuSSH, "root", &invalidTimeoutZero, nil)
		if !errors.Is(err, ErrInvalidConnectionTimeout) {
			t.Errorf("expected ErrInvalidConnectionTimeout, got: %v", err)
		}
	})
}

func TestValidateCredentialTypeCompatibility(t *testing.T) {
	tests := []struct {
		authType AuthType
		credType credDomain.Type
		valid    bool
	}{
		{AuthTypeSSHKey, credDomain.TypeSSHPrivateKey, true},
		{AuthTypeSSHKey, credDomain.TypeSSHPassword, false},
		{AuthTypeSSHKey, credDomain.TypeCPanelAPIToken, false},
		{AuthTypeSSHPassword, credDomain.TypeSSHPassword, true},
		{AuthTypeSSHPassword, credDomain.TypeSSHPrivateKey, false},
		{AuthTypeCPanelAPIToken, credDomain.TypeCPanelAPIToken, true},
		{AuthTypeCPanelAPIToken, credDomain.TypeCPanelPassword, false},
		{AuthTypeCPanelPassword, credDomain.TypeCPanelPassword, true},
		{AuthTypeCPanelPassword, credDomain.TypeCPanelAPIToken, false},
		{AuthType("invalid"), credDomain.TypeSSHPrivateKey, false},
	}

	for _, tc := range tests {
		err := ValidateCredentialTypeCompatibility(tc.authType, tc.credType)
		if tc.valid && err != nil {
			t.Errorf("expected compatibility for %s + %s, got: %v", tc.authType, tc.credType, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("expected incompatibility for %s + %s, got success", tc.authType, tc.credType)
		}
	}
}

func TestValidateResourceName(t *testing.T) {
	name, err := ValidateResourceName("  Valid Server Name  ")
	if err != nil || name != "Valid Server Name" {
		t.Errorf("expected trimmed name, got %s, err: %v", name, err)
	}

	_, err = ValidateResourceName("")
	if !errors.Is(err, ErrInvalidResourceName) {
		t.Errorf("expected ErrInvalidResourceName for empty name")
	}
}

func TestValidateConnectorType(t *testing.T) {
	if err := ValidateConnectorType(TypeUbuntuSSH, ConnectorTypeUbuntuSSH); err != nil {
		t.Errorf("expected valid pair for ubuntu_ssh, got: %v", err)
	}
	if err := ValidateConnectorType(TypeCPanel, ConnectorTypeCPanel); err != nil {
		t.Errorf("expected valid pair for cpanel, got: %v", err)
	}
	if err := ValidateConnectorType(TypeUbuntuSSH, ConnectorTypeCPanel); err == nil {
		t.Errorf("expected error for ubuntu_ssh + cpanel connector")
	}
	if err := ValidateConnectorType(TypeCPanel, ConnectorTypeUbuntuSSH); err == nil {
		t.Errorf("expected error for cpanel + ubuntu_ssh connector")
	}
}

func TestValidateCPanelOperationalUsername(t *testing.T) {
	validCases := []string{
		"mycpanel",
		"cpaneluser123",
		"john_doe",
		"user-name",
		"admin.test",
	}
	for _, u := range validCases {
		if err := ValidateCPanelOperationalUsername(u); err != nil {
			t.Errorf("expected valid username %q, got err: %v", u, err)
		}
	}

	invalidCases := []string{
		"",             // empty
		"MyCPanel",     // uppercase
		"User",         // uppercase
		"user:name",    // colon
		"user\nname",   // LF
		"user\rname",   // CR
		"user\x00name", // NUL
	}
	for _, u := range invalidCases {
		err := ValidateCPanelOperationalUsername(u)
		if !errors.Is(err, ErrInvalidConnectorConfig) {
			t.Errorf("expected ErrInvalidConnectorConfig for %q, got: %v", u, err)
		}
	}
}
