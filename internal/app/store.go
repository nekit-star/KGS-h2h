package app

import (
	"sync"
	"time"

	paymentsgate "example.com/kgs-payment"
)

type CredentialView struct {
	AccountNumber string `json:"account_number,omitempty"`
	AccountOwner  string `json:"account_owner,omitempty"`
	AccountName   string `json:"account_name,omitempty"`
	BankName      string `json:"bank_name,omitempty"`
	QRCode        string `json:"qr_code,omitempty"`
	DeepLink      string `json:"deep_link,omitempty"`
	PaymentURL    string `json:"payment_url,omitempty"`
}

type DealEvent struct {
	Source    string    `json:"source"`
	Status    string    `json:"status,omitempty"`
	Note      string    `json:"note,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type DealRecord struct {
	Profile              string          `json:"profile"`
	DealID               string          `json:"deal_id"`
	InvoiceID            string          `json:"invoice_id,omitempty"`
	ClientID             string          `json:"client_id,omitempty"`
	Direction            string          `json:"direction"`
	Method               string          `json:"method"`
	Status               string          `json:"status,omitempty"`
	Type                 string          `json:"type,omitempty"`
	WidgetURL            string          `json:"widget_url,omitempty"`
	CredentialsPending   bool            `json:"credentials_pending"`
	Credentials          *CredentialView `json:"credentials,omitempty"`
	FinalizationDeadline time.Time       `json:"finalization_deadline,omitempty"`
	LastUpdateAt         time.Time       `json:"last_update_at"`
	Events               []DealEvent     `json:"events"`
}

type Store struct {
	mu        sync.Mutex
	deals     map[string]DealRecord
	processed map[string]struct{}
}

func NewStore() *Store {
	return &Store{
		deals:     make(map[string]DealRecord),
		processed: make(map[string]struct{}),
	}
}

func (s *Store) UpsertCreated(profile, direction, method string, deal *paymentsgate.Deal, finalizationWindow time.Duration, now time.Time) DealRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.deals[deal.ID]
	record.Profile = profile
	record.DealID = deal.ID
	record.InvoiceID = firstNonEmpty(record.InvoiceID, deal.InvoiceID)
	record.ClientID = firstNonEmpty(record.ClientID, deal.ClientID)
	record.Direction = direction
	record.Method = method
	record.Status = firstNonEmpty(string(deal.Status), record.Status)
	record.Type = firstNonEmpty(deal.Type, record.Type)
	record.WidgetURL = firstNonEmpty(deal.URL, record.WidgetURL)
	record.LastUpdateAt = now.UTC()
	if record.FinalizationDeadline.IsZero() && finalizationWindow > 0 {
		record.FinalizationDeadline = now.UTC().Add(finalizationWindow)
	}
	record.Events = append(record.Events, DealEvent{
		Source:    "merchant.create",
		Status:    string(deal.Status),
		Timestamp: now.UTC(),
	})

	s.deals[deal.ID] = record
	return record
}

func (s *Store) AttachCredentials(dealID string, credentials *paymentsgate.TraderCredentials, now time.Time) DealRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.deals[dealID]
	record.CredentialsPending = false
	record.Credentials = &CredentialView{
		AccountNumber: credentials.AccountNumber,
		AccountOwner:  credentials.AccountOwner,
		AccountName:   credentials.AccountName,
		BankName:      credentials.BankName,
		QRCode:        credentials.QRCode,
		DeepLink:      credentials.DeepLink,
		PaymentURL:    credentials.PaymentURL,
	}
	record.LastUpdateAt = now.UTC()
	record.Events = append(record.Events, DealEvent{
		Source:    "paymentsgate.credentials",
		Note:      "trader credentials received",
		Timestamp: now.UTC(),
	})
	s.deals[dealID] = record
	return record
}

func (s *Store) MarkCredentialsPending(dealID string, now time.Time) DealRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.deals[dealID]
	record.CredentialsPending = true
	record.LastUpdateAt = now.UTC()
	record.Events = append(record.Events, DealEvent{
		Source:    "merchant.polling",
		Note:      "trader credentials are still pending",
		Timestamp: now.UTC(),
	})
	s.deals[dealID] = record
	return record
}

func (s *Store) ApplyWebhook(payload paymentsgate.WebhookPayload, dedupeKey string, now time.Time) (DealRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.processed[dedupeKey]; exists {
		return s.deals[payload.ID], true
	}
	s.processed[dedupeKey] = struct{}{}

	record := s.deals[payload.ID]
	record.DealID = firstNonEmpty(record.DealID, payload.ID)
	record.InvoiceID = firstNonEmpty(record.InvoiceID, payload.InvoiceID)
	record.ClientID = firstNonEmpty(record.ClientID, payload.ClientID)
	record.Status = firstNonEmpty(string(payload.Status), record.Status)
	record.Type = firstNonEmpty(payload.Type, record.Type)
	record.LastUpdateAt = now.UTC()
	record.Events = append(record.Events, DealEvent{
		Source:    "paymentsgate.webhook",
		Status:    string(payload.Status),
		Timestamp: now.UTC(),
	})

	s.deals[payload.ID] = record
	return record, false
}

func (s *Store) ApplyRemoteUpdate(profile string, deal *paymentsgate.Deal, source string, finalizationWindow time.Duration, now time.Time) DealRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.deals[deal.ID]
	record.Profile = firstNonEmpty(record.Profile, profile)
	record.DealID = deal.ID
	record.InvoiceID = firstNonEmpty(record.InvoiceID, deal.InvoiceID)
	record.ClientID = firstNonEmpty(record.ClientID, deal.ClientID)
	record.Status = firstNonEmpty(string(deal.Status), record.Status)
	record.Type = firstNonEmpty(deal.Type, record.Type)
	record.WidgetURL = firstNonEmpty(deal.URL, record.WidgetURL)
	record.LastUpdateAt = now.UTC()
	if record.FinalizationDeadline.IsZero() && finalizationWindow > 0 {
		record.FinalizationDeadline = now.UTC().Add(finalizationWindow)
	}
	record.Events = append(record.Events, DealEvent{
		Source:    source,
		Status:    string(deal.Status),
		Timestamp: now.UTC(),
	})
	s.deals[deal.ID] = record
	return record
}

func (s *Store) AppendNote(dealID, source, note string, now time.Time) DealRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	record := s.deals[dealID]
	record.LastUpdateAt = now.UTC()
	record.Events = append(record.Events, DealEvent{
		Source:    source,
		Note:      note,
		Timestamp: now.UTC(),
	})
	s.deals[dealID] = record
	return record
}

func (s *Store) Get(dealID string) (DealRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.deals[dealID]
	return record, ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
