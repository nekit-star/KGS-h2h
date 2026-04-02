package paymentsgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ClientConfig struct {
	BaseURL     string
	Account     ServiceAccount
	HTTPClient  *http.Client
	TokenSource AccessTokenSource
}

type Client struct {
	baseURL     string
	account     ServiceAccount
	httpClient  *http.Client
	tokenSource AccessTokenSource
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.TokenSource == nil {
		manager, err := NewTokenManager(cfg.BaseURL, cfg.Account, cfg.HTTPClient)
		if err != nil {
			return nil, err
		}
		cfg.TokenSource = manager
	}
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		account:     cfg.Account,
		httpClient:  cfg.HTTPClient,
		tokenSource: cfg.TokenSource,
	}, nil
}

func (c *Client) CreatePayIn(ctx context.Context, req CreatePayInRequest) (*Deal, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var out Deal
	if err := c.doJSON(ctx, http.MethodPost, "/deals/payin", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreatePayout(ctx context.Context, req CreatePayoutRequest) (*Deal, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var out Deal
	if err := c.doJSON(ctx, http.MethodPost, "/deals/payout", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetDeal(ctx context.Context, dealID string) (*Deal, error) {
	if strings.TrimSpace(dealID) == "" {
		return nil, ErrDealIDRequired
	}
	var out Deal
	if err := c.doJSON(ctx, http.MethodGet, "/deals/"+url.PathEscape(dealID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetTraderCredentials(ctx context.Context, dealID string) (*TraderCredentials, error) {
	if strings.TrimSpace(dealID) == "" {
		return nil, ErrDealIDRequired
	}
	var out TraderCredentials
	if err := c.doJSON(ctx, http.MethodGet, "/deals/credentials/"+url.PathEscape(dealID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateDealStatus(ctx context.Context, dealID string, req UpdateDealStatusRequest) (*Deal, error) {
	if strings.TrimSpace(dealID) == "" {
		return nil, ErrDealIDRequired
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var out Deal
	if err := c.doJSON(ctx, http.MethodPatch, "/deals/"+url.PathEscape(dealID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelDeal(ctx context.Context, dealID string) (*Deal, error) {
	if strings.TrimSpace(dealID) == "" {
		return nil, ErrDealIDRequired
	}
	var out Deal
	if err := c.doJSON(ctx, http.MethodDelete, "/deals/"+url.PathEscape(dealID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	token, err := c.tokenSource.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("resolve access token: %w", err)
	}

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("accept", "application/json")
	request.Header.Set("authorization", "Bearer "+token)
	if payload != nil {
		request.Header.Set("content-type", "application/json")
	}

	response, err := c.httpClient.Do(request)
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
