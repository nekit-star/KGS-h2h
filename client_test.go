package paymentsgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestCreatePayInSetsAuthorizationAndParsesResponse(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.URL.Path, "/deals/payin"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("authorization"), "Bearer token-123"; got != want {
			t.Fatalf("authorization = %q, want %q", got, want)
		}
		var payload CreatePayInRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if got, want := payload.Type, "elqr"; got != want {
			t.Fatalf("type = %q, want %q", got, want)
		}
		return jsonResponse(t, http.StatusOK, map[string]any{
			"id":        "deal-1",
			"status":    "new",
			"type":      "elqr",
			"url":       "https://widget.example/deal-1",
			"invoiceId": "inv-1",
			"clientId":  "client-1",
		}), nil
	})}

	client, err := NewClient(ClientConfig{
		BaseURL:     "http://provider.test",
		TokenSource: staticTokenSource("token-123"),
		HTTPClient:  httpClient,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	deal, err := client.CreatePayIn(context.Background(), CreatePayInRequest{
		Amount:    100,
		Currency:  "KGS",
		InvoiceID: "inv-1",
		ClientID:  "client-1",
		Type:      "elqr",
	})
	if err != nil {
		t.Fatalf("CreatePayIn() error = %v", err)
	}
	if got, want := deal.ID, "deal-1"; got != want {
		t.Fatalf("deal.ID = %q, want %q", got, want)
	}
	if got, want := deal.URL, "https://widget.example/deal-1"; got != want {
		t.Fatalf("deal.URL = %q, want %q", got, want)
	}
}

func TestGetTraderCredentialsReturnsAPIErrorOnNotFound(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(bytes.NewReader([]byte(`{"error":"ERROR_NOT_FOUND","message":"not ready","statusCode":404}`))),
		}, nil
	})}

	client, err := NewClient(ClientConfig{
		BaseURL:     "http://provider.test",
		TokenSource: staticTokenSource("token-123"),
		HTTPClient:  httpClient,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetTraderCredentials(context.Background(), "deal-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GetTraderCredentials() error = %T, want *APIError", err)
	}
	if got, want := apiErr.StatusCode, http.StatusNotFound; got != want {
		t.Fatalf("apiErr.StatusCode = %d, want %d", got, want)
	}
}
