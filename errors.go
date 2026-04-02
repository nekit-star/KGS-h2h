package paymentsgate

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrEmptyBody                  = errors.New("empty request body")
	ErrInvalidJSON                = errors.New("invalid json body")
	ErrMissingAPIKey              = errors.New("missing x-api-key header")
	ErrMissingSignature           = errors.New("missing x-api-signature header")
	ErrUnknownAPIKey              = errors.New("unknown x-api-key")
	ErrInvalidSignatureEncoding   = errors.New("invalid x-api-signature encoding")
	ErrSignatureMismatch          = errors.New("webhook signature mismatch")
	ErrPrivateKeyRequired         = errors.New("private key is required")
	ErrUnsupportedPrivateKey      = errors.New("unsupported private key format")
	ErrPublicKeyRequired          = errors.New("public key is required")
	ErrAccountIDRequired          = errors.New("account id is required")
	ErrCredentialsNotReady        = errors.New("trader credentials are not ready")
	ErrProfileNotConfigured       = errors.New("profile is not configured")
	ErrDealIDRequired             = errors.New("deal id is required")
	ErrWebhookVerificationSkipped = errors.New("webhook signature verification skipped")
)

type ProviderError struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode"`
}

type APIError struct {
	StatusCode int
	Body       string
	Provider   ProviderError
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if strings.TrimSpace(e.Provider.Error) != "" || strings.TrimSpace(e.Provider.Message) != "" {
		return fmt.Sprintf("paymentsgate api returned http %d: %s (%s)", e.StatusCode, firstNonEmpty(e.Provider.Message, e.Body), e.Provider.Error)
	}
	if strings.TrimSpace(e.Body) != "" {
		return fmt.Sprintf("paymentsgate api returned http %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("paymentsgate api returned http %d", e.StatusCode)
}

func (e *APIError) IsStatus(statusCode int) bool {
	return e != nil && e.StatusCode == statusCode
}

func HTTPStatusFromWebhookError(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrMissingAPIKey), errors.Is(err, ErrMissingSignature), errors.Is(err, ErrInvalidJSON), errors.Is(err, ErrEmptyBody):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnknownAPIKey):
		return http.StatusUnauthorized
	case errors.Is(err, ErrSignatureMismatch), errors.Is(err, ErrInvalidSignatureEncoding), errors.Is(err, ErrUnsupportedPrivateKey):
		return http.StatusForbidden
	default:
		return http.StatusConflict
	}
}
