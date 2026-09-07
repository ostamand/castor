package crypto

import (
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

// Keypair contains the public recipient string and secret key string
type Keypair struct {
	PublicKey string
	SecretKey string
}

// GenerateKeypair generates a new asymmetric X25519 Age keypair
func GenerateKeypair() (*Keypair, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("failed to generate X25519 keypair: %w", err)
	}

	return &Keypair{
		PublicKey: identity.Recipient().String(),
		SecretKey: identity.String(),
	}, nil
}

// EncryptStream wraps an io.Writer with an Age multi-recipient encryption stream
func EncryptStream(w io.Writer, publicKeys []string) (io.WriteCloser, error) {
	if len(publicKeys) == 0 {
		return nil, fmt.Errorf("at least one Age public key is required for encryption")
	}

	var recipients []age.Recipient
	for _, k := range publicKeys {
		clean := strings.TrimSpace(k)
		if clean == "" {
			continue
		}
		r, err := age.ParseX25519Recipient(clean)
		if err != nil {
			return nil, fmt.Errorf("invalid Age public key '%s': %w", clean, err)
		}
		recipients = append(recipients, r)
	}

	if len(recipients) == 0 {
		return nil, fmt.Errorf("no valid Age recipients found")
	}

	return age.Encrypt(w, recipients...)
}

// DecryptStream wraps an io.Reader with an Age decryption stream
func DecryptStream(r io.Reader, secretKey string) (io.ReadCloser, error) {
	cleanKey := strings.TrimSpace(secretKey)
	if cleanKey == "" {
		return nil, fmt.Errorf("secret key is required for decryption")
	}

	identity, err := age.ParseX25519Identity(cleanKey)
	if err != nil {
		return nil, fmt.Errorf("invalid Age secret key: %w", err)
	}

	reader, err := age.Decrypt(r, identity)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize decryption: %w", err)
	}

	return io.NopCloser(reader), nil
}

// ParseRecipientOrIdentity validates and extracts an Age public key (age1...)
// from either a public key string or a secret key string (AGE-SECRET-KEY-1...)
func ParseRecipientOrIdentity(input string) (string, error) {
	clean := strings.TrimSpace(input)
	if strings.HasPrefix(clean, "age1") {
		_, err := age.ParseX25519Recipient(clean)
		if err != nil {
			return "", fmt.Errorf("invalid Age public key: %w", err)
		}
		return clean, nil
	}

	if strings.HasPrefix(clean, "AGE-SECRET-KEY-1") {
		identity, err := age.ParseX25519Identity(clean)
		if err != nil {
			return "", fmt.Errorf("invalid Age secret key: %w", err)
		}
		return identity.Recipient().String(), nil
	}

	return "", fmt.Errorf("key must begin with 'age1' (public key) or 'AGE-SECRET-KEY-1' (secret key)")
}
