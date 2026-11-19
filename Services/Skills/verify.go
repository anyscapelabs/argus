package Skills

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	_ "embed"
	"strings"
)

//go:embed public_key.pem
var publicKeyPEM []byte

var signingKey = func() ed25519.PublicKey {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil
	}
	return key
}()

// VerifyIndex checks the detached Ed25519 signature over the exact index bytes.
// A missing or malformed trust anchor fails closed.
func VerifyIndex(body, signature []byte) bool {
	if signingKey == nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(signingKey, body, signature)
}

func BundleSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func VerifyBundle(body []byte, want string) bool {
	got := BundleSHA256(body)
	return subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(strings.TrimSpace(want)))) == 1
}
