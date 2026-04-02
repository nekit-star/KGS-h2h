package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	paymentsgate "example.com/kgs-payment"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCreatePayInPollsUntilCredentialsArrive(t *testing.T) {
	t.Parallel()

	credentialCalls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/auth/token":
			return jsonResponse(t, http.StatusOK, map[string]any{
				"access_token":  "access-1",
				"refresh_token": "refresh-1",
				"expires_in":    3600,
			}), nil
		case "/deals/payin":
			return jsonResponse(t, http.StatusOK, map[string]any{
				"id":        "deal-1",
				"status":    "new",
				"type":      "elqr",
				"url":       "https://widget.example/deal-1",
				"invoiceId": "inv-1",
				"clientId":  "client-1",
			}), nil
		case "/deals/credentials/deal-1":
			credentialCalls++
			if credentialCalls < 3 {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(bytes.NewReader([]byte(`{"error":"ERROR_NOT_FOUND","message":"not ready","statusCode":404}`))),
				}, nil
			}
			return jsonResponse(t, http.StatusOK, map[string]any{
				"account_number": "A21200398479",
				"account_owner":  "Alikhan Khanov ELQR",
				"qrCode":         "qr-blob",
			}), nil
		default:
			t.Fatalf("unexpected provider path %s", r.URL.Path)
			return nil, nil
		}
	})}

	service, err := New(Config{
		Environment: paymentsgate.Environment{
			BaseURL:                 "http://provider.test",
			HTTPTimeout:             5 * time.Second,
			CredentialsPollInterval: time.Millisecond,
			CredentialsWait:         time.Second,
			Profiles: map[string]paymentsgate.ServiceAccount{
				"kgs_primary": {
					Name:      "kgs_primary",
					AccountID: "account-1",
					PublicKey: "PUBLIC_KEY_B64",
				},
			},
		},
		HTTPClient: httpClient,
		Now:        time.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record, err := service.CreatePayIn(context.Background(), CreatePayInInput{
		Method:    "elqr",
		Amount:    61,
		Currency:  "KGS",
		InvoiceID: "inv-1",
		ClientID:  "client-1",
	})
	if err != nil {
		t.Fatalf("CreatePayIn() error = %v", err)
	}
	if got, want := record.DealID, "deal-1"; got != want {
		t.Fatalf("record.DealID = %q, want %q", got, want)
	}
	if record.Credentials == nil {
		t.Fatal("expected credentials to be attached")
	}
	if got, want := record.Credentials.AccountNumber, "A21200398479"; got != want {
		t.Fatalf("AccountNumber = %q, want %q", got, want)
	}
	if credentialCalls < 3 {
		t.Fatalf("expected polling to retry, got %d credential calls", credentialCalls)
	}
}

func TestHandleWebhookRequestDeduplicatesPayload(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	service, err := New(Config{
		Environment: paymentsgate.Environment{
			BaseURL:                 "http://provider.test",
			HTTPTimeout:             5 * time.Second,
			CredentialsPollInterval: time.Millisecond,
			CredentialsWait:         time.Second,
			Profiles: map[string]paymentsgate.ServiceAccount{
				"kgs_primary": {
					Name:       "kgs_primary",
					AccountID:  "account-1",
					PublicKey:  "PUBLIC_KEY_B64",
					PrivateKey: string(privateKeyPEM),
				},
			},
		},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("provider should not be called in webhook test")
		})},
		Now: time.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	service.store.UpsertCreated("kgs_primary", "payin", "elqr", &paymentsgate.Deal{
		ID:     "deal-1",
		Status: paymentsgate.StatusNew,
		Type:   "elqr",
	}, 10*time.Minute, time.Now())

	body := []byte(`{"id":"deal-1","status":"completed","invoiceId":"inv-1","clientId":"client-1","type":"elqr"}`)
	checksum, err := paymentsgate.ChecksumSHA256(body)
	if err != nil {
		t.Fatalf("ChecksumSHA256() error = %v", err)
	}
	encrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privateKey.PublicKey, []byte(checksum), nil)
	if err != nil {
		t.Fatalf("rsa.EncryptOAEP() error = %v", err)
	}

	request := newWebhookRequest(t, body, "account-1", base64.StdEncoding.EncodeToString(encrypted))
	record, duplicate, err := service.HandleWebhookRequest(request)
	if err != nil {
		t.Fatalf("HandleWebhookRequest() error = %v", err)
	}
	if duplicate {
		t.Fatal("first webhook must not be duplicate")
	}
	if got, want := record.Status, "completed"; got != want {
		t.Fatalf("record.Status = %q, want %q", got, want)
	}

	request = newWebhookRequest(t, body, "account-1", base64.StdEncoding.EncodeToString(encrypted))
	_, duplicate, err = service.HandleWebhookRequest(request)
	if err != nil {
		t.Fatalf("HandleWebhookRequest() second call error = %v", err)
	}
	if !duplicate {
		t.Fatal("second webhook must be duplicate")
	}
}

func TestHandleWebhookRequestForwardsCompletedWebhook(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	forwardCalls := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "webhook.test":
			forwardCalls++
			if got, want := r.URL.Path, "/success"; got != want {
				t.Fatalf("forward path = %q, want %q", got, want)
			}
			if got, want := r.Header.Get("x-kgs-payment-forwarded-status"), "completed"; got != want {
				t.Fatalf("forwarded status = %q, want %q", got, want)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("io.ReadAll() error = %v", err)
			}
			if !bytes.Contains(body, []byte(`"status":"completed"`)) {
				t.Fatalf("forwarded body = %s, want completed payload", string(body))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     http.Header{},
			}, nil
		default:
			return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
		}
	})}

	service, err := New(Config{
		Environment: paymentsgate.Environment{
			BaseURL:                 "http://provider.test",
			HTTPTimeout:             5 * time.Second,
			CredentialsPollInterval: time.Millisecond,
			CredentialsWait:         time.Second,
			SuccessWebhookURL:       "http://webhook.test/success",
			FailWebhookURL:          "http://webhook.test/fail",
			Profiles: map[string]paymentsgate.ServiceAccount{
				"kgs_primary": {
					Name:       "kgs_primary",
					AccountID:  "account-1",
					PublicKey:  "PUBLIC_KEY_B64",
					PrivateKey: string(privateKeyPEM),
				},
			},
		},
		HTTPClient: httpClient,
		Now:        time.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	service.store.UpsertCreated("kgs_primary", "payin", "elqr", &paymentsgate.Deal{
		ID:     "deal-1",
		Status: paymentsgate.StatusNew,
		Type:   "elqr",
	}, 10*time.Minute, time.Now())

	body := []byte(`{"id":"deal-1","status":"completed","invoiceId":"inv-1","clientId":"client-1","type":"elqr"}`)
	checksum, err := paymentsgate.ChecksumSHA256(body)
	if err != nil {
		t.Fatalf("ChecksumSHA256() error = %v", err)
	}
	encrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privateKey.PublicKey, []byte(checksum), nil)
	if err != nil {
		t.Fatalf("rsa.EncryptOAEP() error = %v", err)
	}

	request := newWebhookRequest(t, body, "account-1", base64.StdEncoding.EncodeToString(encrypted))
	_, _, err = service.HandleWebhookRequest(request)
	if err != nil {
		t.Fatalf("HandleWebhookRequest() error = %v", err)
	}
	if got, want := forwardCalls, 1; got != want {
		t.Fatalf("forward calls = %d, want %d", got, want)
	}
}

func newWebhookRequest(t *testing.T, body []byte, apiKey, signature string) *http.Request {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, "https://merchant.example/webhooks/paymentsgate", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("x-api-key", apiKey)
	request.Header.Set("x-api-signature", signature)
	return request
}

func jsonResponse(t *testing.T, statusCode int, payload any) *http.Response {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}
}
