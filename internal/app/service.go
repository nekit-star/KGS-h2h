package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	paymentsgate "example.com/kgs-payment"
)

const (
	defaultPayInFinalizationWindow  = 10 * time.Minute
	defaultPayoutFinalizationWindow = 12 * time.Hour
)

var defaultProfileMap = map[string]string{
	"elqr_primary":     "kgs_primary",
	"elqr_ftd":         "kgs_primary",
	"elqr_trusted":     "kgs_primary",
	"kgsphone_primary": "kgs_primary",
	"kgsphone_ftd":     "kgs_primary",
	"kgsphone_trusted": "kgs_phone_trusted",
	"p2p_primary":      "kgs_p2p_ftd",
	"p2p_ftd":          "kgs_p2p_ftd",
	"p2p_trusted":      "kgs_p2p_trusted",
	"odengi_primary":   "kgs_odengi_ftd",
	"odengi_ftd":       "kgs_odengi_ftd",
	"odengi_trusted":   "kgs_odengi_trusted",
}

type Config struct {
	Environment paymentsgate.Environment
	HTTPClient  *http.Client
	Now         func() time.Time
}

type CreatePayInInput struct {
	Method                 string   `json:"method"`
	TrafficType            string   `json:"traffic_type,omitempty"`
	CredentialProfile      string   `json:"credential_profile,omitempty"`
	Amount                 int64    `json:"amount"`
	Currency               string   `json:"currency,omitempty"`
	Country                string   `json:"country,omitempty"`
	InvoiceID              string   `json:"invoice_id"`
	ClientID               string   `json:"client_id"`
	ClientCard             string   `json:"client_card,omitempty"`
	ClientName             string   `json:"client_name,omitempty"`
	SuccessURL             string   `json:"success_url,omitempty"`
	FailURL                string   `json:"fail_url,omitempty"`
	BackURL                string   `json:"back_url,omitempty"`
	Lang                   string   `json:"lang,omitempty"`
	ELQRBanks              []string `json:"elqr_banks,omitempty"`
	WaitForCredentials     *bool    `json:"wait_for_credentials,omitempty"`
	CredentialsWaitSeconds int      `json:"credentials_wait_seconds,omitempty"`
}

type CreatePayoutInput struct {
	Method            string                 `json:"method"`
	TrafficType       string                 `json:"traffic_type,omitempty"`
	CredentialProfile string                 `json:"credential_profile,omitempty"`
	Amount            int64                  `json:"amount"`
	Currency          string                 `json:"currency,omitempty"`
	CurrencyTo        string                 `json:"currency_to,omitempty"`
	InvoiceID         string                 `json:"invoice_id,omitempty"`
	ClientID          string                 `json:"client_id"`
	SenderName        string                 `json:"sender_name,omitempty"`
	BaseCurrency      string                 `json:"base_currency,omitempty"`
	FeesStrategy      string                 `json:"fees_strategy,omitempty"`
	Recipient         paymentsgate.Recipient `json:"recipient"`
}

type FinalizeDealInput struct {
	CredentialProfile string                  `json:"credential_profile,omitempty"`
	Status            paymentsgate.DealStatus `json:"status"`
}

type Service struct {
	environment  paymentsgate.Environment
	clients      map[string]*paymentsgate.Client
	httpClient   *http.Client
	store        *Store
	verifier     *paymentsgate.WebhookVerifier
	now          func() time.Time
	pollInterval time.Duration
	waitTimeout  time.Duration
}

func New(cfg Config) (*Service, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Environment.HTTPTimeout}
	}
	if len(cfg.Environment.Profiles) == 0 {
		return nil, fmt.Errorf("no paymentsgate profiles configured; create .env from .env.example or add KGS_* terminal variables")
	}

	clients := make(map[string]*paymentsgate.Client, len(cfg.Environment.Profiles))
	for name, account := range cfg.Environment.Profiles {
		client, err := paymentsgate.NewClient(paymentsgate.ClientConfig{
			BaseURL:    cfg.Environment.BaseURL,
			Account:    account,
			HTTPClient: cfg.HTTPClient,
		})
		if err != nil {
			return nil, fmt.Errorf("build client for profile %s: %w", name, err)
		}
		clients[name] = client
	}

	verifier, err := paymentsgate.NewWebhookVerifier(func(apiKey string) (string, error) {
		normalized := strings.ToLower(strings.TrimSpace(apiKey))
		for _, account := range cfg.Environment.Profiles {
			lookupKey := strings.ToLower(strings.TrimSpace(account.APIKey))
			if lookupKey == "" {
				lookupKey = strings.ToLower(account.AccountID)
			}
			if normalized == lookupKey {
				if strings.TrimSpace(account.PrivateKeyValue()) == "" {
					return "", paymentsgate.ErrPrivateKeyRequired
				}
				return account.PrivateKeyValue(), nil
			}
		}
		return "", paymentsgate.ErrUnknownAPIKey
	}, cfg.Environment.AllowUnsignedWebhooks)
	if err != nil {
		return nil, err
	}

	pollInterval := cfg.Environment.CredentialsPollInterval
	if pollInterval <= 0 {
		pollInterval = 1500 * time.Millisecond
	}
	waitTimeout := cfg.Environment.CredentialsWait
	if waitTimeout <= 0 {
		waitTimeout = 20 * time.Second
	}

	return &Service{
		environment:  cfg.Environment,
		clients:      clients,
		httpClient:   cfg.HTTPClient,
		store:        NewStore(),
		verifier:     verifier,
		now:          cfg.Now,
		pollInterval: pollInterval,
		waitTimeout:  waitTimeout,
	}, nil
}

func (s *Service) CreatePayIn(ctx context.Context, input CreatePayInInput) (DealRecord, error) {
	method := normalizeMethod(input.Method)
	if err := validateMethod(method); err != nil {
		return DealRecord{}, err
	}
	if input.Amount <= 0 {
		return DealRecord{}, fmt.Errorf("amount must be greater than zero")
	}
	if strings.TrimSpace(input.InvoiceID) == "" {
		return DealRecord{}, fmt.Errorf("invoice_id is required")
	}
	if strings.TrimSpace(input.ClientID) == "" {
		return DealRecord{}, fmt.Errorf("client_id is required")
	}

	profileName, client, err := s.resolveClient(method, input.TrafficType, input.CredentialProfile)
	if err != nil {
		return DealRecord{}, err
	}

	request := paymentsgate.CreatePayInRequest{
		Amount:     input.Amount,
		Currency:   firstNonEmpty(input.Currency, "KGS"),
		Country:    input.Country,
		InvoiceID:  input.InvoiceID,
		ClientID:   input.ClientID,
		Type:       method,
		ClientCard: input.ClientCard,
		ClientName: input.ClientName,
		SuccessURL: input.SuccessURL,
		FailURL:    input.FailURL,
		BackURL:    input.BackURL,
		Lang:       input.Lang,
	}
	if method == "elqr" && len(input.ELQRBanks) > 0 {
		request.MultiWidgetOptions = &paymentsgate.MultiWidgetOptions{ELQRBanks: input.ELQRBanks}
	}

	deal, err := client.CreatePayIn(ctx, request)
	if err != nil {
		return DealRecord{}, err
	}

	record := s.store.UpsertCreated(profileName, "payin", method, deal, defaultPayInFinalizationWindow, s.now())

	shouldWait := true
	if input.WaitForCredentials != nil {
		shouldWait = *input.WaitForCredentials
	}
	if !shouldWait {
		return record, nil
	}

	waitTimeout := s.waitTimeout
	if input.CredentialsWaitSeconds > 0 {
		waitTimeout = time.Duration(input.CredentialsWaitSeconds) * time.Second
	}

	credentials, err := s.pollCredentials(ctx, client, deal.ID, waitTimeout)
	if err != nil {
		if errors.Is(err, paymentsgate.ErrCredentialsNotReady) {
			return s.store.MarkCredentialsPending(deal.ID, s.now()), nil
		}
		return record, err
	}
	return s.store.AttachCredentials(deal.ID, credentials, s.now()), nil
}

func (s *Service) CreatePayout(ctx context.Context, input CreatePayoutInput) (DealRecord, error) {
	method := normalizeMethod(input.Method)
	if err := validateMethod(method); err != nil {
		return DealRecord{}, err
	}
	if input.Amount <= 0 {
		return DealRecord{}, fmt.Errorf("amount must be greater than zero")
	}
	if strings.TrimSpace(input.ClientID) == "" {
		return DealRecord{}, fmt.Errorf("client_id is required")
	}

	profileName, client, err := s.resolveClient(method, input.TrafficType, input.CredentialProfile)
	if err != nil {
		return DealRecord{}, err
	}

	request := paymentsgate.CreatePayoutRequest{
		Currency:     input.Currency,
		Amount:       input.Amount,
		CurrencyTo:   firstNonEmpty(input.CurrencyTo, "KGS"),
		InvoiceID:    input.InvoiceID,
		ClientID:     input.ClientID,
		SenderName:   input.SenderName,
		BaseCurrency: firstNonEmpty(input.BaseCurrency, "fiat"),
		FeesStrategy: firstNonEmpty(input.FeesStrategy, "sub"),
		Recipient:    input.Recipient,
		Type:         method,
	}
	deal, err := client.CreatePayout(ctx, request)
	if err != nil {
		return DealRecord{}, err
	}
	return s.store.UpsertCreated(profileName, "payout", method, deal, defaultPayoutFinalizationWindow, s.now()), nil
}

func (s *Service) FinalizeDeal(ctx context.Context, dealID string, input FinalizeDealInput) (DealRecord, error) {
	if strings.TrimSpace(dealID) == "" {
		return DealRecord{}, paymentsgate.ErrDealIDRequired
	}

	profileName := strings.ToLower(strings.TrimSpace(input.CredentialProfile))
	if profileName == "" {
		record, ok := s.store.Get(dealID)
		if !ok || record.Profile == "" {
			return DealRecord{}, fmt.Errorf("profile is required to finalize deal %s", dealID)
		}
		profileName = record.Profile
	}

	client, ok := s.clients[profileName]
	if !ok {
		return DealRecord{}, fmt.Errorf("%w: %s", paymentsgate.ErrProfileNotConfigured, profileName)
	}

	deal, err := client.UpdateDealStatus(ctx, dealID, paymentsgate.UpdateDealStatusRequest{Status: input.Status})
	if err != nil {
		return DealRecord{}, err
	}
	return s.store.ApplyRemoteUpdate(profileName, deal, "merchant.finalize", 0, s.now()), nil
}

func (s *Service) GetDeal(dealID string) (DealRecord, bool) {
	return s.store.Get(dealID)
}

func (s *Service) HandleWebhookRequest(r *http.Request) (DealRecord, bool, error) {
	verified, err := s.verifier.VerifyRequest(r)
	if err != nil {
		return DealRecord{}, false, err
	}
	sum := sha256.Sum256(verified.RawBody)
	dedupeKey := hex.EncodeToString(sum[:])
	record, duplicate := s.store.ApplyWebhook(verified.Payload, verified.APIKey+":"+dedupeKey, s.now())
	if !duplicate {
		if err := s.forwardFinalWebhook(r.Context(), verified.Payload, verified.RawBody); err != nil {
			record = s.store.AppendNote(record.DealID, "merchant.forward", err.Error(), s.now())
		}
	}
	return record, duplicate, nil
}

func (s *Service) pollCredentials(ctx context.Context, client *paymentsgate.Client, dealID string, waitTimeout time.Duration) (*paymentsgate.TraderCredentials, error) {
	deadline := s.now().Add(waitTimeout)
	for {
		credentials, err := client.GetTraderCredentials(ctx, dealID)
		if err == nil && credentials != nil && credentials.HasValue() {
			return credentials, nil
		}
		if err != nil {
			var apiErr *paymentsgate.APIError
			if !errors.As(err, &apiErr) || !apiErr.IsStatus(http.StatusNotFound) {
				return nil, err
			}
		}
		if !s.now().Before(deadline) {
			return nil, paymentsgate.ErrCredentialsNotReady
		}

		timer := time.NewTimer(s.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) resolveClient(method, traffic, explicitProfile string) (string, *paymentsgate.Client, error) {
	if explicitProfile != "" {
		name := strings.ToLower(strings.TrimSpace(explicitProfile))
		client, ok := s.clients[name]
		if !ok {
			return "", nil, fmt.Errorf("%w: %s", paymentsgate.ErrProfileNotConfigured, name)
		}
		return name, client, nil
	}

	traffic = normalizeTraffic(traffic)
	candidateKeys := []string{
		method + "_" + traffic,
		method + "_primary",
	}
	for _, key := range candidateKeys {
		profile := strings.ToLower(strings.TrimSpace(s.environment.ProfileMap[strings.ToLower(key)]))
		if profile == "" {
			profile = defaultProfileMap[key]
		}
		if profile == "" {
			continue
		}
		client, ok := s.clients[profile]
		if ok {
			return profile, client, nil
		}
	}
	return "", nil, fmt.Errorf("%w for method=%s traffic=%s", paymentsgate.ErrProfileNotConfigured, method, traffic)
}

func normalizeMethod(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeTraffic(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "primary":
		return "primary"
	case "ftd":
		return "ftd"
	case "trusted":
		return "trusted"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func validateMethod(value string) error {
	switch value {
	case "elqr", "kgsphone", "p2p", "odengi":
		return nil
	default:
		return fmt.Errorf("unsupported method %q", value)
	}
}

func (s *Service) forwardFinalWebhook(ctx context.Context, payload paymentsgate.WebhookPayload, body []byte) error {
	var destination string
	switch {
	case payload.Status.IsSuccess():
		destination = strings.TrimSpace(s.environment.SuccessWebhookURL)
	case payload.Status.IsFailure():
		destination = strings.TrimSpace(s.environment.FailWebhookURL)
	default:
		return nil
	}
	if destination == "" {
		return nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, destination, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create forwarded webhook request: %w", err)
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("x-kgs-payment-forwarded-status", string(payload.Status))
	request.Header.Set("x-kgs-payment-forwarded-deal-id", payload.ID)

	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("forward webhook: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(response.Body)
		return fmt.Errorf("forward webhook returned http %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
