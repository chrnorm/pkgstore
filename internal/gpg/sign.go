package gpg

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

// Signer signs APT Release files using a PGP private key.
type Signer struct {
	entity *openpgp.Entity
}

// NewSigner creates a Signer from an ASCII-armored PGP private key.
// If the key is passphrase-protected, pass the passphrase; otherwise pass "".
func NewSigner(armoredKey string, passphrase string) (*Signer, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredKey))
	if err != nil {
		return nil, fmt.Errorf("reading GPG key: %w", err)
	}
	if len(entities) == 0 {
		return nil, fmt.Errorf("no GPG keys found")
	}

	entity := entities[0]

	if entity.PrivateKey == nil {
		return nil, fmt.Errorf("key has no private key")
	}

	if entity.PrivateKey.Encrypted {
		if passphrase == "" {
			return nil, fmt.Errorf("key is encrypted but no passphrase provided")
		}
		if err := entity.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("decrypting private key: %w", err)
		}
	}

	return &Signer{entity: entity}, nil
}

// DetachedSign produces an ASCII-armored detached signature of the data (Release.gpg).
func (s *Signer) DetachedSign(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, s.entity, bytes.NewReader(data), nil); err != nil {
		return nil, fmt.Errorf("creating detached signature: %w", err)
	}
	return buf.Bytes(), nil
}

// ClearSign produces a clearsigned version of the data (InRelease).
func (s *Signer) ClearSign(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := clearsign.Encode(&buf, s.entity.PrivateKey, nil)
	if err != nil {
		return nil, fmt.Errorf("creating clearsign encoder: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("writing clearsign data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing clearsign writer: %w", err)
	}
	return buf.Bytes(), nil
}

// KeyID returns the hex key ID of the signing key.
func (s *Signer) KeyID() string {
	return fmt.Sprintf("%X", s.entity.PrimaryKey.KeyId)
}

// ArmoredPublicKey returns the ASCII-armored public key, suitable for distribution
// to users who need to verify the repository signatures.
func (s *Signer) ArmoredPublicKey() (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		return "", err
	}
	if err := s.entity.Serialize(w); err != nil {
		w.Close()
		return "", fmt.Errorf("serializing public key: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// VerifyDetachedSignature verifies a detached signature against the signed data.
// publicKey is the ASCII-armored public key. Returns nil if valid.
func VerifyDetachedSignature(publicKey string, data []byte, signature []byte) error {
	kr, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicKey))
	if err != nil {
		return fmt.Errorf("reading public key: %w", err)
	}
	_, err = openpgp.CheckArmoredDetachedSignature(kr, bytes.NewReader(data), bytes.NewReader(signature), nil)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}

// VerifyClearSignature verifies a clearsigned message.
// publicKey is the ASCII-armored public key. Returns the signed body if valid.
func VerifyClearSignature(publicKey string, signed []byte) ([]byte, error) {
	kr, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicKey))
	if err != nil {
		return nil, fmt.Errorf("reading public key: %w", err)
	}

	block, _ := clearsign.Decode(signed)
	if block == nil {
		return nil, fmt.Errorf("no clearsigned data found")
	}

	_, err = openpgp.CheckDetachedSignature(kr, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, nil)
	if err != nil {
		return nil, fmt.Errorf("clearsign verification failed: %w", err)
	}

	return block.Bytes, nil
}

// GenerateTestKey creates a new PGP key pair for testing purposes.
// Returns the ASCII-armored private key.
func GenerateTestKey() (string, error) {
	entity, err := openpgp.NewEntity("Test", "", "test@example.com", nil)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP PRIVATE KEY BLOCK", nil)
	if err != nil {
		return "", err
	}
	if err := entity.SerializePrivate(w, nil); err != nil {
		w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// ReadPublicKeyFromPrivate extracts the armored public key from an armored private key.
func ReadPublicKeyFromPrivate(armoredPrivateKey string) (string, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredPrivateKey))
	if err != nil {
		return "", err
	}
	if len(entities) == 0 {
		return "", fmt.Errorf("no keys found")
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		return "", err
	}
	if err := entities[0].Serialize(w); err != nil {
		w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ReadArmoredKeyRing is a convenience wrapper around openpgp.ReadArmoredKeyRing.
func ReadArmoredKeyRing(r io.Reader) (openpgp.EntityList, error) {
	return openpgp.ReadArmoredKeyRing(r)
}
