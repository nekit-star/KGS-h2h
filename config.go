package paymentsgate

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type ServiceAccount struct {
	Name           string
	TerminalCode   string
	APIKey         string
	AccountID      string
	MerchantID     string
	ProjectID      string
	Realm          string
	BeginPublicKey string
	RSAPrivateKey  string
	PublicKey      string
	PrivateKey     string
}

func (a ServiceAccount) ValidateForAuth() error {
	switch {
	case strings.TrimSpace(a.AccountID) == "":
		return ErrAccountIDRequired
	case strings.TrimSpace(a.PublicKeyValue()) == "":
		return ErrPublicKeyRequired
	default:
		return nil
	}
}

func (a ServiceAccount) PublicKeyValue() string {
	return normalizeKeyText(firstNonEmpty(a.BeginPublicKey, a.PublicKey))
}

func (a ServiceAccount) PrivateKeyValue() string {
	return normalizeKeyText(firstNonEmpty(a.RSAPrivateKey, a.PrivateKey))
}

func (a ServiceAccount) EncodedPublicKey() string {
	key := a.PublicKeyValue()
	if key == "" {
		return ""
	}

	decoded, err := base64.StdEncoding.DecodeString(key)
	if err == nil && bytes.Contains(decoded, []byte("BEGIN PUBLIC KEY")) {
		return key
	}
	if strings.Contains(key, "BEGIN PUBLIC KEY") {
		return base64.StdEncoding.EncodeToString([]byte(key))
	}
	return key
}

type Environment struct {
	BaseURL                 string
	HTTPTimeout             time.Duration
	CredentialsPollInterval time.Duration
	CredentialsWait         time.Duration
	AllowedClockSkew        time.Duration
	AllowUnsignedWebhooks   bool
	SuccessWebhookURL       string
	FailWebhookURL          string
	Profiles                map[string]ServiceAccount
	ProfileMap              map[string]string
}

func LoadEnvironment(path string) (Environment, error) {
	values := map[string]string{}
	if strings.TrimSpace(path) != "" {
		loaded, err := loadEnvFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return Environment{}, fmt.Errorf("read env file: %w", err)
			}
		} else {
			values = loaded
		}
	}

	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if ok {
			values[key] = value
		}
	}

	httpTimeoutSeconds, err := parseInt(values, "PAYMENTSGATE_HTTP_TIMEOUT_SECONDS", 15)
	if err != nil {
		return Environment{}, err
	}
	pollIntervalMillis, err := parseInt(values, "PAYMENTSGATE_CREDENTIALS_POLL_INTERVAL_MS", 1500)
	if err != nil {
		return Environment{}, err
	}
	waitSeconds, err := parseInt(values, "PAYMENTSGATE_CREDENTIALS_WAIT_SECONDS", 20)
	if err != nil {
		return Environment{}, err
	}
	clockSkewSeconds, err := parseInt(values, "PAYMENTSGATE_ALLOWED_CLOCK_SKEW_SECONDS", 0)
	if err != nil {
		return Environment{}, err
	}

	profiles := loadProfiles(values)
	profileMap := loadProfileMap(values)

	return Environment{
		BaseURL:                 firstNonEmpty(values["PAYMENTSGATE_BASE_URL"], "https://secure.easysendglobal.com"),
		HTTPTimeout:             time.Duration(httpTimeoutSeconds) * time.Second,
		CredentialsPollInterval: time.Duration(pollIntervalMillis) * time.Millisecond,
		CredentialsWait:         time.Duration(waitSeconds) * time.Second,
		AllowedClockSkew:        time.Duration(clockSkewSeconds) * time.Second,
		AllowUnsignedWebhooks:   parseBool(values["PAYMENTSGATE_ALLOW_UNSIGNED_WEBHOOKS"]),
		SuccessWebhookURL:       firstNonEmpty(values["PAYMENTSGATE_SUCCESS_WEBHOOK_URL"], "https://webhook.site/b71e1b94-2e0b-43b1-8544-b9fe3325db9b"),
		FailWebhookURL:          firstNonEmpty(values["PAYMENTSGATE_FAIL_WEBHOOK_URL"], "https://webhook.site/b71e1b94-2e0b-43b1-8544-b9fe3325db9b"),
		Profiles:                profiles,
		ProfileMap:              profileMap,
	}, nil
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeTerminalName(value string) string {
	name := normalizeName(value)
	if name == "" {
		return ""
	}
	if !strings.HasPrefix(name, "kgs_") {
		name = "kgs_" + name
	}
	return name
}

func normalizeKeyText(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.ReplaceAll(trimmed, `\n`, "\n")
	return trimmed
}

func loadProfiles(values map[string]string) map[string]ServiceAccount {
	profiles := map[string]ServiceAccount{}

	legacyFieldMap := map[string]func(*ServiceAccount, string){
		"API_KEY":          func(a *ServiceAccount, v string) { a.APIKey = v },
		"TERMINAL_CODE":    func(a *ServiceAccount, v string) { a.TerminalCode = v },
		"ACCOUNT_ID":       func(a *ServiceAccount, v string) { a.AccountID = v },
		"MERCHANT_ID":      func(a *ServiceAccount, v string) { a.MerchantID = v },
		"PROJECT_ID":       func(a *ServiceAccount, v string) { a.ProjectID = v },
		"REALM":            func(a *ServiceAccount, v string) { a.Realm = v },
		"PUBLIC_KEY":       func(a *ServiceAccount, v string) { a.PublicKey = v },
		"PRIVATE_KEY":      func(a *ServiceAccount, v string) { a.PrivateKey = v },
		"BEGIN_PUBLIC_KEY": func(a *ServiceAccount, v string) { a.BeginPublicKey = v },
		"RSA_PRIVATE_KEY":  func(a *ServiceAccount, v string) { a.RSAPrivateKey = v },
	}
	collectAccounts(values, profiles, "PAYMENTSGATE_PROFILE_", legacyFieldMap, func(raw string) string {
		return normalizeName(raw)
	})

	terminalFieldMap := map[string]func(*ServiceAccount, string){
		"TERMINAL_CODE":    func(a *ServiceAccount, v string) { a.TerminalCode = v },
		"API_KEY":          func(a *ServiceAccount, v string) { a.APIKey = v },
		"ACCOUNT_ID":       func(a *ServiceAccount, v string) { a.AccountID = v },
		"MERCHANT_ID":      func(a *ServiceAccount, v string) { a.MerchantID = v },
		"PROJECT_ID":       func(a *ServiceAccount, v string) { a.ProjectID = v },
		"REALM":            func(a *ServiceAccount, v string) { a.Realm = v },
		"BEGIN_PUBLIC_KEY": func(a *ServiceAccount, v string) { a.BeginPublicKey = v },
		"RSA_PRIVATE_KEY":  func(a *ServiceAccount, v string) { a.RSAPrivateKey = v },
		"PUBLIC_KEY":       func(a *ServiceAccount, v string) { a.PublicKey = v },
		"PRIVATE_KEY":      func(a *ServiceAccount, v string) { a.PrivateKey = v },
	}
	collectAccounts(values, profiles, "KGS_", terminalFieldMap, normalizeTerminalName)

	for name, account := range profiles {
		account.Name = name
		if strings.TrimSpace(account.TerminalCode) == "" {
			account.TerminalCode = name
		}
		profiles[name] = account
	}

	return profiles
}

func collectAccounts(values map[string]string, accounts map[string]ServiceAccount, prefix string, fieldMap map[string]func(*ServiceAccount, string), normalize func(string) string) {
	for key, value := range values {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		if strings.HasPrefix(rest, "METHOD_") {
			continue
		}
		for field, assign := range fieldMap {
			suffix := "_" + field
			if !strings.HasSuffix(rest, suffix) {
				continue
			}
			name := normalize(strings.TrimSuffix(rest, suffix))
			if name == "" {
				continue
			}
			account := accounts[name]
			account.Name = name
			assign(&account, value)
			accounts[name] = account
		}
	}
}

func loadProfileMap(values map[string]string) map[string]string {
	profileMap := map[string]string{}

	for key, value := range values {
		if strings.HasPrefix(key, "PAYMENTSGATE_PROFILE_MAP_") {
			profileMap[normalizeName(strings.TrimPrefix(key, "PAYMENTSGATE_PROFILE_MAP_"))] = normalizeProfileTarget(value)
			continue
		}
		if strings.HasPrefix(key, "KGS_METHOD_") && strings.HasSuffix(key, "_TERMINAL") {
			routeKey := normalizeName(strings.TrimSuffix(strings.TrimPrefix(key, "KGS_METHOD_"), "_TERMINAL"))
			profileMap[routeKey] = normalizeProfileTarget(value)
		}
	}

	return profileMap
}

func normalizeProfileTarget(value string) string {
	target := normalizeName(value)
	if target == "" {
		return ""
	}
	if !strings.HasPrefix(target, "kgs_") {
		target = "kgs_" + target
	}
	return target
}

func loadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env line %d", lineNumber)
		}
		values[strings.TrimSpace(key)] = trimEnvQuotes(strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func trimEnvQuotes(value string) string {
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func parseInt(values map[string]string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
