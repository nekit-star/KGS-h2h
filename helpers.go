package paymentsgate

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Body:       strings.TrimSpace(string(body)),
	}

	var provider ProviderError
	if err := json.Unmarshal(body, &provider); err == nil {
		apiErr.Provider = provider
		if provider.StatusCode == 0 {
			apiErr.Provider.StatusCode = statusCode
		}
	}
	return apiErr
}

func coalesceString(candidates []map[string]any, keys ...string) string {
	for _, candidate := range candidates {
		for _, key := range keys {
			if value, ok := candidate[key]; ok {
				switch typed := value.(type) {
				case string:
					if strings.TrimSpace(typed) != "" {
						return typed
					}
				case json.Number:
					return typed.String()
				case float64:
					return fmt.Sprintf("%.0f", typed)
				}
			}
		}
	}
	return ""
}

func coalesceNumber(candidates []map[string]any, keys ...string) json.Number {
	for _, candidate := range candidates {
		for _, key := range keys {
			if value, ok := candidate[key]; ok {
				switch typed := value.(type) {
				case json.Number:
					return typed
				case float64:
					return json.Number(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", typed), "0"), "."))
				case string:
					if strings.TrimSpace(typed) != "" {
						return json.Number(strings.TrimSpace(typed))
					}
				}
			}
		}
	}
	return ""
}

func candidateMaps(raw map[string]any) []map[string]any {
	candidates := []map[string]any{raw}
	for _, key := range []string{"deal", "data", "payload", "result", "recipient"} {
		if child, ok := raw[key].(map[string]any); ok {
			candidates = append(candidates, child)
		}
	}
	return candidates
}

func decodeRawObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()

	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}
