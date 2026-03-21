package gpg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAndSign(t *testing.T) {
	privKey, err := GenerateTestKey()
	require.NoError(t, err)
	require.NotEmpty(t, privKey)

	signer, err := NewSigner(privKey, "")
	require.NoError(t, err)

	data := []byte("Origin: Test\nSuite: stable\nAcquire-By-Hash: yes\n")

	// Test detached signature.
	sig, err := signer.DetachedSign(data)
	require.NoError(t, err)
	assert.Contains(t, string(sig), "BEGIN PGP SIGNATURE")

	// Test clearsign.
	clearSigned, err := signer.ClearSign(data)
	require.NoError(t, err)
	assert.Contains(t, string(clearSigned), "BEGIN PGP SIGNED MESSAGE")
	assert.Contains(t, string(clearSigned), "Origin: Test")

	// Test key ID.
	keyID := signer.KeyID()
	assert.NotEmpty(t, keyID)
}

func TestVerifyDetachedSignature(t *testing.T) {
	privKey, err := GenerateTestKey()
	require.NoError(t, err)

	signer, err := NewSigner(privKey, "")
	require.NoError(t, err)

	pubKey, err := signer.ArmoredPublicKey()
	require.NoError(t, err)

	data := []byte("test data for signing")
	sig, err := signer.DetachedSign(data)
	require.NoError(t, err)

	// Should verify successfully.
	err = VerifyDetachedSignature(pubKey, data, sig)
	assert.NoError(t, err)

	// Should fail with tampered data.
	err = VerifyDetachedSignature(pubKey, []byte("tampered"), sig)
	assert.Error(t, err)
}

func TestVerifyClearSignature(t *testing.T) {
	privKey, err := GenerateTestKey()
	require.NoError(t, err)

	signer, err := NewSigner(privKey, "")
	require.NoError(t, err)

	pubKey, err := signer.ArmoredPublicKey()
	require.NoError(t, err)

	data := []byte("test data for clearsigning")
	clearSigned, err := signer.ClearSign(data)
	require.NoError(t, err)

	// Should verify successfully.
	body, err := VerifyClearSignature(pubKey, clearSigned)
	require.NoError(t, err)
	assert.Equal(t, data, body)
}
