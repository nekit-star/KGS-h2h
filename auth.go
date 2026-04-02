package paymentsgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AccessTokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

type TokenManager struct {
	baseURL    string
	account    ServiceAccount
	httpClient *http.Client
	now        func() time.Time

	mu        sync.Mutex
	token     TokenResponse
	expiresAt time.Time
}

func NewTokenManager(baseURL string, account ServiceAccount, httpClient *http.Client) (*TokenManager, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &TokenManager{
		baseURL:    strings.TrimRight(baseURL, "/"),
		account:    account,
		httpClient: httpClient,
		now:        time.Now,
	}, nil
}

func (m *TokenManager) AccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.token.AccessToken != "" && m.now().Before(m.expiresAt.Add(-30*time.Second)) {
		return m.token.AccessToken, nil
	}

	if m.token.RefreshToken != "" {
		token, err := m.refreshLocked(ctx)
		if err == nil {
			return token.AccessToken, nil
		}
	}

	token, err := m.issueLocked(ctx)
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (m *TokenManager) IssueToken(ctx context.Context) (*TokenResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.issueLocked(ctx)
}

func (m *TokenManager) RefreshToken(ctx context.Context) (*TokenResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshLocked(ctx)
}

func (m *TokenManager) RevokeToken(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(m.token.RefreshToken) == "" {
		return nil
	}
	return m.doAuthJSON(ctx, http.MethodPost, "/auth/token/revoke", m.token.AccessToken, map[string]string{
		"refresh_token": m.token.RefreshToken,
	}, nil)
}

func (m *TokenManager) ValidateToken(ctx context.Context) (*ValidateTokenResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out ValidateTokenResponse
	if err := m.doAuthJSON(ctx, http.MethodGet, "/auth/token/validate", m.token.AccessToken, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *TokenManager) issueLocked(ctx context.Context) (*TokenResponse, error) {
	if err := m.account.ValidateForAuth(); err != nil {
		return nil, err
	}
	var out TokenResponse
	if err := m.doAuthJSON(ctx, http.MethodPost, "/auth/token", "", map[string]string{
		"account_id": m.account.AccountID,
		"public_key": m.account.EncodedPublicKey(),
	}, &out); err != nil {
		return nil, err
	}
	m.installToken(out)
	return &out, nil
}

func (m *TokenManager) refreshLocked(ctx context.Context) (*TokenResponse, error) {
	if strings.TrimSpace(m.token.RefreshToken) == "" {
		return nil, fmt.Errorf("refresh token is empty")
	}
	var out TokenResponse
	if err := m.doAuthJSON(ctx, http.MethodPost, "/auth/token/refresh", m.token.AccessToken, map[string]string{
		"refresh_token": m.token.RefreshToken,
	}, &out); err != nil {
		return nil, err
	}
	m.installToken(out)
	return &out, nil
}

func (m *TokenManager) installToken(token TokenResponse) {
	m.token = token
	m.expiresAt = m.now().Add(time.Duration(token.ExpiresIn) * time.Second)
}

func (m *TokenManager) doAuthJSON(ctx context.Context, method, path, accessToken string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	request, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("accept", "application/json")
	if payload != nil {
		request.Header.Set("content-type", "application/json")
	}
	if strings.TrimSpace(accessToken) != "" {
		request.Header.Set("authorization", "Bearer "+accessToken)
	}

	response, err := m.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return parseAPIError(response.StatusCode, responseBody)
	}
	if out == nil || len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
