package paymentsgate

import (
	"encoding/json"
	"fmt"
	"strings"
)

type DealStatus string

const (
	StatusQueued    DealStatus = "queued"
	StatusNew       DealStatus = "new"
	StatusPending   DealStatus = "pending"
	StatusPaid      DealStatus = "paid"
	StatusExpired   DealStatus = "expired"
	StatusCompleted DealStatus = "completed"
	StatusCanceled  DealStatus = "canceled"
)

func (s DealStatus) IsFinal() bool {
	return s == StatusCompleted || s == StatusCanceled
}

func (s DealStatus) IsSuccess() bool {
	return s == StatusCompleted
}

func (s DealStatus) IsFailure() bool {
	return s == StatusCanceled || s == StatusExpired
}

func (s DealStatus) CanTransitionTo(next DealStatus) bool {
	switch s {
	case StatusQueued, StatusNew, StatusPending:
		return next == StatusCompleted || next == StatusCanceled
	case StatusPaid:
		return next == StatusCompleted
	case StatusExpired:
		return next == StatusCompleted || next == StatusCanceled
	default:
		return false
	}
}

type MultiWidgetOptions struct {
	ELQRBanks []string `json:"elqrBanks,omitempty"`
}

type CreatePayInRequest struct {
	Amount             int64               `json:"amount"`
	Currency           string              `json:"currency"`
	Country            string              `json:"country,omitempty"`
	InvoiceID          string              `json:"invoiceId"`
	ClientID           string              `json:"clientId"`
	Type               string              `json:"type"`
	ClientCard         string              `json:"clientCard,omitempty"`
	ClientName         string              `json:"clientName,omitempty"`
	SuccessURL         string              `json:"successUrl,omitempty"`
	FailURL            string              `json:"failUrl,omitempty"`
	BackURL            string              `json:"backUrl,omitempty"`
	Lang               string              `json:"lang,omitempty"`
	Trusted            *bool               `json:"trusted,omitempty"`
	BankID             string              `json:"bankId,omitempty"`
	MultiWidgetOptions *MultiWidgetOptions `json:"multiWidgetOptions,omitempty"`
}

func (r CreatePayInRequest) Validate() error {
	switch {
	case r.Amount <= 0:
		return fmt.Errorf("amount must be greater than zero")
	case strings.TrimSpace(r.Currency) == "":
		return fmt.Errorf("currency is required")
	case strings.TrimSpace(r.InvoiceID) == "":
		return fmt.Errorf("invoiceId is required")
	case strings.TrimSpace(r.ClientID) == "":
		return fmt.Errorf("clientId is required")
	case strings.TrimSpace(r.Type) == "":
		return fmt.Errorf("type is required")
	default:
		return nil
	}
}

type Recipient struct {
	AccountNumber  string `json:"account_number,omitempty"`
	AccountOwner   string `json:"account_owner,omitempty"`
	AccountIBAN    string `json:"account_iban,omitempty"`
	AccountPhone   string `json:"account_phone,omitempty"`
	Type           string `json:"type,omitempty"`
	AccountBankID  string `json:"account_bank_id,omitempty"`
	AccountEmail   string `json:"account_email,omitempty"`
	AccountEwallet string `json:"account_ewallet_name,omitempty"`
}

type CreatePayoutRequest struct {
	Currency        string    `json:"currency,omitempty"`
	Amount          int64     `json:"amount"`
	CurrencyTo      string    `json:"currencyTo"`
	InvoiceID       string    `json:"invoiceId,omitempty"`
	ClientID        string    `json:"clientId"`
	SenderName      string    `json:"sender_name,omitempty"`
	Recipient       Recipient `json:"recipient"`
	TTL             int64     `json:"ttl,omitempty"`
	TTLUnit         string    `json:"ttl_unit,omitempty"`
	SrcAmount       int64     `json:"src_amount,omitempty"`
	FinalAmount     int64     `json:"finalAmount,omitempty"`
	BaseCurrency    string    `json:"baseCurrency,omitempty"`
	FiatLiquidity   *bool     `json:"fiatLiquidity,omitempty"`
	FeesStrategy    string    `json:"feesStrategy,omitempty"`
	RefundAvailable *bool     `json:"refundAvailable,omitempty"`
	Type            string    `json:"type,omitempty"`
}

func (r CreatePayoutRequest) Validate() error {
	switch {
	case r.Amount <= 0:
		return fmt.Errorf("amount must be greater than zero")
	case strings.TrimSpace(r.CurrencyTo) == "":
		return fmt.Errorf("currencyTo is required")
	case strings.TrimSpace(r.ClientID) == "":
		return fmt.Errorf("clientId is required")
	case strings.TrimSpace(r.Recipient.AccountNumber) == "":
		return fmt.Errorf("recipient.account_number is required")
	case strings.TrimSpace(r.Recipient.Type) == "":
		return fmt.Errorf("recipient.type is required")
	default:
		return nil
	}
}

type UpdateDealStatusRequest struct {
	Status DealStatus `json:"status"`
}

func (r UpdateDealStatusRequest) Validate() error {
	if strings.TrimSpace(string(r.Status)) == "" {
		return fmt.Errorf("status is required")
	}
	switch r.Status {
	case StatusCompleted, StatusCanceled:
		return nil
	default:
		return fmt.Errorf("unsupported status transition target %q", r.Status)
	}
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type ValidateTokenResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

type Deal struct {
	ID         string         `json:"id,omitempty"`
	Status     DealStatus     `json:"status,omitempty"`
	Type       string         `json:"type,omitempty"`
	URL        string         `json:"url,omitempty"`
	InvoiceID  string         `json:"invoiceId,omitempty"`
	ClientID   string         `json:"clientId,omitempty"`
	Currency   string         `json:"currency,omitempty"`
	CurrencyTo string         `json:"currencyTo,omitempty"`
	Amount     json.Number    `json:"amount,omitempty"`
	Recipient  *Recipient     `json:"recipient,omitempty"`
	Raw        map[string]any `json:"-"`
}

func (d *Deal) UnmarshalJSON(data []byte) error {
	raw, err := decodeRawObject(data)
	if err != nil {
		return err
	}
	candidates := candidateMaps(raw)

	d.Raw = raw
	d.ID = coalesceString(candidates, "id", "dealId", "deal_id")
	d.Status = DealStatus(coalesceString(candidates, "status"))
	d.Type = coalesceString(candidates, "type")
	d.URL = coalesceString(candidates, "url", "redirectUrl", "redirect_url", "paymentUrl", "payment_url")
	d.InvoiceID = coalesceString(candidates, "invoiceId", "invoice_id")
	d.ClientID = coalesceString(candidates, "clientId", "client_id")
	d.Currency = coalesceString(candidates, "currency")
	d.CurrencyTo = coalesceString(candidates, "currencyTo", "currency_to")
	d.Amount = coalesceNumber(candidates, "amount")

	if rawRecipient, ok := raw["recipient"].(map[string]any); ok {
		d.Recipient = &Recipient{
			AccountNumber:  coalesceString([]map[string]any{rawRecipient}, "account_number", "accountNumber"),
			AccountOwner:   coalesceString([]map[string]any{rawRecipient}, "account_owner", "accountOwner"),
			AccountIBAN:    coalesceString([]map[string]any{rawRecipient}, "account_iban", "accountIban"),
			AccountPhone:   coalesceString([]map[string]any{rawRecipient}, "account_phone", "accountPhone"),
			Type:           coalesceString([]map[string]any{rawRecipient}, "type"),
			AccountBankID:  coalesceString([]map[string]any{rawRecipient}, "account_bank_id", "accountBankId"),
			AccountEmail:   coalesceString([]map[string]any{rawRecipient}, "account_email", "accountEmail"),
			AccountEwallet: coalesceString([]map[string]any{rawRecipient}, "account_ewallet_name", "accountEwalletName"),
		}
	}
	return nil
}

type TraderCredentials struct {
	AccountNumber string         `json:"account_number,omitempty"`
	AccountOwner  string         `json:"account_owner,omitempty"`
	AccountName   string         `json:"account_name,omitempty"`
	Type          string         `json:"type,omitempty"`
	BankName      string         `json:"bank_name,omitempty"`
	QRCode        string         `json:"qr_code,omitempty"`
	DeepLink      string         `json:"deep_link,omitempty"`
	PaymentURL    string         `json:"payment_url,omitempty"`
	Raw           map[string]any `json:"-"`
}

func (c *TraderCredentials) UnmarshalJSON(data []byte) error {
	raw, err := decodeRawObject(data)
	if err != nil {
		return err
	}
	candidates := candidateMaps(raw)
	c.Raw = raw
	c.AccountNumber = coalesceString(candidates, "account_number", "accountNumber", "phone", "recipient")
	c.AccountOwner = coalesceString(candidates, "account_owner", "accountOwner", "recipientName", "recipient_name")
	c.AccountName = coalesceString(candidates, "account_name", "accountName")
	c.Type = coalesceString(candidates, "type")
	c.BankName = coalesceString(candidates, "bank_name", "bankName")
	c.QRCode = coalesceString(candidates, "qrCode", "qr_code", "qr", "qrData", "qr_data")
	c.DeepLink = coalesceString(candidates, "deepLink", "deeplink", "deep_link")
	c.PaymentURL = coalesceString(candidates, "paymentUrl", "payment_url", "url", "redirectUrl", "redirect_url")
	return nil
}

func (c TraderCredentials) HasValue() bool {
	return strings.TrimSpace(c.AccountNumber) != "" ||
		strings.TrimSpace(c.QRCode) != "" ||
		strings.TrimSpace(c.PaymentURL) != "" ||
		strings.TrimSpace(c.DeepLink) != ""
}

type WebhookPayload struct {
	ID        string         `json:"id,omitempty"`
	Status    DealStatus     `json:"status,omitempty"`
	Type      string         `json:"type,omitempty"`
	InvoiceID string         `json:"invoiceId,omitempty"`
	ClientID  string         `json:"clientId,omitempty"`
	Amount    json.Number    `json:"amount,omitempty"`
	Currency  string         `json:"currency,omitempty"`
	Raw       map[string]any `json:"-"`
}

func (w *WebhookPayload) UnmarshalJSON(data []byte) error {
	raw, err := decodeRawObject(data)
	if err != nil {
		return err
	}
	candidates := candidateMaps(raw)
	w.Raw = raw
	w.ID = coalesceString(candidates, "id", "dealId", "deal_id")
	w.Status = DealStatus(coalesceString(candidates, "status"))
	w.Type = coalesceString(candidates, "type")
	w.InvoiceID = coalesceString(candidates, "invoiceId", "invoice_id")
	w.ClientID = coalesceString(candidates, "clientId", "client_id")
	w.Amount = coalesceNumber(candidates, "amount")
	w.Currency = coalesceString(candidates, "currency")
	return nil
}
