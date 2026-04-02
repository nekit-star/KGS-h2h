package paymentsgate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestTokenManagerIssuesAndRefreshesToken(t *testing.T) {
	t.Parallel()

	var now = time.Unix(1716299720, 0)
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/auth/token":
			if got, want := r.Method, http.MethodPost; got != want {
				t.Fatalf("issue method = %s, want %s", got, want)
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode issue payload: %v", err)
			}
			if got, want := payload["account_id"], "account-1"; got != want {
				t.Fatalf("account_id = %q, want %q", got, want)
			}
			if got, want := payload["public_key"], "PUBLIC_KEY_B64"; got != want {
				t.Fatalf("public_key = %q, want %q", got, want)
			}
			return jsonResponse(t, http.StatusOK, TokenResponse{
				AccessToken:  "access-1",
				RefreshToken: "refresh-1",
				ExpiresIn:    3600,
			}), nil
		case "/auth/token/refresh":
			if got, want := r.Header.Get("authorization"), "Bearer access-1"; got != want {
				t.Fatalf("refresh authorization = %q, want %q", got, want)
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode refresh payload: %v", err)
			}
			if got, want := payload["refresh_token"], "refresh-1"; got != want {
				t.Fatalf("refresh_token = %q, want %q", got, want)
			}
			return jsonResponse(t, http.StatusOK, TokenResponse{
				AccessToken:  "access-2",
				RefreshToken: "refresh-2",
				ExpiresIn:    3600,
			}), nil
		default:
			t.Fatalf("unexpected auth path %s", r.URL.Path)
			return nil, nil
		}
	})}

	manager, err := NewTokenManager("http://provider.test", ServiceAccount{
		AccountID: "account-1",
		PublicKey: "PUBLIC_KEY_B64",
	}, httpClient)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	manager.now = func() time.Time { return now }

	token, err := manager.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken() issue error = %v", err)
	}
	if got, want := token, "access-1"; got != want {
		t.Fatalf("issued access token = %q, want %q", got, want)
	}

	now = now.Add(2 * time.Hour)
	token, err = manager.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken() refresh error = %v", err)
	}
	if got, want := token, "access-2"; got != want {
		t.Fatalf("refreshed access token = %q, want %q", got, want)
	}
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
