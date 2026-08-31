package httpapi

import (
	"backup-platform/internal/credential/domain"
	"backup-platform/internal/credential/secretcrypto"

	"golang.org/x/crypto/ssh"
)

// processSSHKey parses the SSH private key (with or without passphrase),
// validates its cryptographic correctness, and extracts its SHA256 public key fingerprint.
// Temporary byte slices created during parsing are zeroed with best effort.
func processSSHKey(secret string, passphrase *string) (*string, error) {
	secretBytes := []byte(secret)
	defer secretcrypto.ZeroBytes(secretBytes)

	var signer ssh.Signer
	var err error

	if passphrase != nil && len(*passphrase) > 0 {
		passBytes := []byte(*passphrase)
		defer secretcrypto.ZeroBytes(passBytes)
		signer, err = ssh.ParsePrivateKeyWithPassphrase(secretBytes, passBytes)
	} else {
		signer, err = ssh.ParsePrivateKey(secretBytes)
	}

	if err != nil {
		return nil, domain.ErrInvalidSSHKey
	}

	pubKey := signer.PublicKey()
	fp := ssh.FingerprintSHA256(pubKey)
	return &fp, nil
}
