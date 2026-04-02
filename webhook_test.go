package paymentsgate

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"testing"
)

func TestWebhookVerifierVerifiesWebhookV3Signature(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	verifier, err := NewWebhookVerifier(func(apiKey string) (string, error) {
		if apiKey != "account-1" {
			return "", ErrUnknownAPIKey
		}
		return string(privateKeyPEM), nil
	}, false)
	if err != nil {
		t.Fatalf("NewWebhookVerifier() error = %v", err)
	}

	body := []byte(`{"id":"deal-1","status":"completed","invoiceId":"inv-1","clientId":"client-1","type":"p2p"}`)
	checksum, err := ChecksumSHA256(body)
	if err != nil {
		t.Fatalf("ChecksumSHA256() error = %v", err)
	}
	encrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privateKey.PublicKey, []byte(checksum), nil)
	if err != nil {
		t.Fatalf("rsa.EncryptOAEP() error = %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, "https://merchant.example/webhooks/paymentsgate", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("x-api-key", "account-1")
	request.Header.Set("x-api-signature", base64.StdEncoding.EncodeToString(encrypted))

	verified, err := verifier.VerifyRequest(request)
	if err != nil {
		t.Fatalf("VerifyRequest() error = %v", err)
	}
	if !verified.Verified {
		t.Fatal("expected webhook to be verified")
	}
	if got, want := verified.Payload.ID, "deal-1"; got != want {
		t.Fatalf("payload.ID = %q, want %q", got, want)
	}
}

func TestWebhookVerifierAllowsUnsignedWhenConfigured(t *testing.T) {
	t.Parallel()

	verifier, err := NewWebhookVerifier(func(apiKey string) (string, error) {
		return "", ErrUnknownAPIKey
	}, true)
	if err != nil {
		t.Fatalf("NewWebhookVerifier() error = %v", err)
	}

	body := []byte(`{"id":"deal-1","status":"new"}`)
	verified, err := verifier.Verify(nil, body, http.Header{})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.Verified {
		t.Fatal("expected unsigned webhook to be marked as unverified")
	}
}
