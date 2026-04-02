package paymentsgate

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type PrivateKeyLookup func(apiKey string) (string, error)

type VerifiedWebhook struct {
	APIKey    string
	Signature string
	Verified  bool
	RawBody   []byte
	Payload   WebhookPayload
}

type WebhookVerifier struct {
	lookupPrivateKey      PrivateKeyLookup
	allowUnsignedWebhooks bool
}

func NewWebhookVerifier(lookupPrivateKey PrivateKeyLookup, allowUnsignedWebhooks bool) (*WebhookVerifier, error) {
	if lookupPrivateKey == nil {
		return nil, fmt.Errorf("private key lookup is required")
	}
	return &WebhookVerifier{
		lookupPrivateKey:      lookupPrivateKey,
		allowUnsignedWebhooks: allowUnsignedWebhooks,
	}, nil
}

func (v *WebhookVerifier) VerifyRequest(r *http.Request) (*VerifiedWebhook, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return v.Verify(context.Background(), body, r.Header)
}

func (v *WebhookVerifier) Verify(_ context.Context, body []byte, headers http.Header) (*VerifiedWebhook, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, ErrEmptyBody
	}

	apiKey := strings.TrimSpace(headers.Get("x-api-key"))
	signature := strings.TrimSpace(headers.Get("x-api-signature"))

	if apiKey == "" {
		if !v.allowUnsignedWebhooks {
			return nil, ErrMissingAPIKey
		}
		payload, err := parseWebhookPayload(body)
		if err != nil {
			return nil, err
		}
		return &VerifiedWebhook{
			Verified: false,
			RawBody:  body,
			Payload:  payload,
		}, nil
	}
	if signature == "" {
		return nil, ErrMissingSignature
	}

	privateKeyRaw, err := v.lookupPrivateKey(apiKey)
	if err != nil {
		return nil, err
	}
	privateKey, err := ParseRSAPrivateKey(privateKeyRaw)
	if err != nil {
		return nil, err
	}

	encryptedSignature, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return nil, ErrInvalidSignatureEncoding
	}

	decrypted, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, encryptedSignature, nil)
	if err != nil {
		return nil, ErrSignatureMismatch
	}

	calculated, err := ChecksumSHA256(body)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(string(decrypted))), []byte(calculated)) != 1 {
		return nil, ErrSignatureMismatch
	}

	payload, err := parseWebhookPayload(body)
	if err != nil {
		return nil, err
	}

	return &VerifiedWebhook{
		APIKey:    apiKey,
		Signature: signature,
		Verified:  true,
		RawBody:   body,
		Payload:   payload,
	}, nil
}

func parseWebhookPayload(body []byte) (WebhookPayload, error) {
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return WebhookPayload{}, ErrInvalidJSON
	}
	return payload, nil
}

func ParseRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	normalized := normalizeKeyText(raw)
	if normalized == "" {
		return nil, ErrPrivateKeyRequired
	}

	data := []byte(normalized)
	if !bytes.Contains(data, []byte("BEGIN")) {
		decoded, err := base64.StdEncoding.DecodeString(normalized)
		if err != nil {
			return nil, ErrUnsupportedPrivateKey
		}
		data = decoded
	}

	if block, _ := pem.Decode(data); block != nil {
		data = block.Bytes
	}

	if key, err := x509.ParsePKCS1PrivateKey(data); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(data)
	if err != nil {
		return nil, ErrUnsupportedPrivateKey
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrUnsupportedPrivateKey
	}
	return privateKey, nil
}
