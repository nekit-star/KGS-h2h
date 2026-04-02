package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	paymentsgate "example.com/kgs-payment"
)

type Application struct {
	service *Service
}

func NewApplication(cfg Config) (*Application, error) {
	service, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &Application{service: service}, nil
}

func (a *Application) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/payins", a.handleCreatePayIn)
	mux.HandleFunc("/payouts", a.handleCreatePayout)
	mux.HandleFunc("/deals/", a.handleDeal)
	mux.HandleFunc("/webhooks/paymentsgate", a.handleWebhook)
	return mux
}

func (a *Application) handleCreatePayIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input CreatePayInInput
	if err := decodeJSON(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	record, err := a.service.CreatePayIn(r.Context(), input)
	if err != nil {
		writeAppError(w, err)
		return
	}

	statusCode := http.StatusOK
	if record.CredentialsPending {
		statusCode = http.StatusAccepted
	}
	writeJSON(w, statusCode, record)
}

func (a *Application) handleCreatePayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input CreatePayoutInput
	if err := decodeJSON(r, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	record, err := a.service.CreatePayout(r.Context(), input)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (a *Application) handleDeal(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/deals/") {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/deals/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(path, "/status") {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		dealID := strings.TrimSuffix(path, "/status")
		dealID = strings.TrimSuffix(dealID, "/")
		var input FinalizeDealInput
		if err := decodeJSON(r, &input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		record, err := a.service.FinalizeDeal(r.Context(), dealID, input)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	record, ok := a.service.GetDeal(path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (a *Application) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	record, duplicate, err := a.service.HandleWebhookRequest(r)
	if err != nil {
		http.Error(w, err.Error(), paymentsgate.HTTPStatusFromWebhookError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"duplicate": duplicate,
		"deal":      record,
	})
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAppError(w http.ResponseWriter, err error) {
	var apiErr *paymentsgate.APIError
	if errors.As(err, &apiErr) {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":      apiErr.Provider.Error,
			"message":    firstNonEmpty(apiErr.Provider.Message, apiErr.Body),
			"statusCode": apiErr.StatusCode,
		})
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}
