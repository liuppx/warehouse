package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testBase58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base64URLEncode(input []byte) string {
	return base64.RawURLEncoding.EncodeToString(input)
}

func base58Encode(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	digits := []byte{0}
	for _, b := range input {
		carry := int(b)
		for i := 0; i < len(digits); i++ {
			carry += int(digits[i]) * 256
			digits[i] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			digits = append(digits, byte(carry%58))
			carry /= 58
		}
	}
	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}
	var builder strings.Builder
	for i := 0; i < zeros; i++ {
		builder.WriteByte('1')
	}
	for i := len(digits) - 1; i >= 0; i-- {
		builder.WriteByte(testBase58Alphabet[digits[i]])
	}
	return builder.String()
}

func didKeyFromEd25519PublicKey(publicKey ed25519.PublicKey) string {
	prefix := []byte{0xed, 0x01}
	payload := append(prefix, publicKey...)
	return "did:key:z" + base58Encode(payload)
}

func buildProoflessCentralToken(t *testing.T, audience string, capabilities []UcanCapability) (token string, issuer string) {
	t.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	issuer = didKeyFromEd25519PublicKey(publicKey)

	nowSec := time.Now().Unix()
	header := map[string]any{
		"alg": "EdDSA",
		"typ": "UCAN",
		"ucv": "0.10.0",
	}
	payload := map[string]any{
		"iss": issuer,
		"aud": audience,
		"sub": "0x5c7bf91c493126314bb821c123dee889ffca3932",
		"cap": capabilities,
		"nbf": nowSec - 60,
		"exp": nowSec + 600,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header failed: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	signingInput := base64URLEncode(headerJSON) + "." + base64URLEncode(payloadJSON)
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	token = signingInput + "." + base64URLEncode(signature)
	return token, issuer
}

func TestUcanVerifierTrustedIssuerAllowsProoflessCentralToken(t *testing.T) {
	audience := "did:web:localhost:6065"
	token, issuer := buildProoflessCentralToken(t, audience, []UcanCapability{
		{With: "app:all:localhost-3020", Can: "write"},
	})

	verifier := NewUcanVerifier(
		true,
		audience,
		[]UcanCapability{{With: "app:*", Can: "read,write"}},
		[]string{issuer},
		nil,
	)
	address, err := verifier.VerifyInvocation(token)
	if err != nil {
		t.Fatalf("VerifyInvocation failed: %v", err)
	}
	if address != "0x5c7bf91c493126314bb821c123dee889ffca3932" {
		t.Fatalf("address = %q, want %q", address, "0x5c7bf91c493126314bb821c123dee889ffca3932")
	}
}

func TestUcanVerifierWithoutTrustedIssuerRejectsProoflessCentralToken(t *testing.T) {
	audience := "did:web:localhost:6065"
	token, _ := buildProoflessCentralToken(t, audience, []UcanCapability{
		{With: "app:all:localhost-3020", Can: "write"},
	})

	verifier := NewUcanVerifier(
		true,
		audience,
		[]UcanCapability{{With: "app:*", Can: "read,write"}},
		nil,
		nil,
	)
	_, err := verifier.VerifyInvocation(token)
	if err == nil {
		t.Fatalf("expected VerifyInvocation to fail without trusted issuer")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "proof chain") {
		t.Fatalf("unexpected error: %v", err)
	}
}
