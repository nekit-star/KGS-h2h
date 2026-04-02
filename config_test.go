package paymentsgate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvironmentSupportsTerminalStyleVariables(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := `PAYMENTSGATE_BASE_URL=https://secure.easysendglobal.com
KGS_METHOD_ELQR_PRIMARY_TERMINAL=primary
KGS_PRIMARY_TERMINAL_CODE=12345
KGS_PRIMARY_ACCOUNT_ID=account-1
KGS_PRIMARY_BEGIN_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----\nABC\n-----END PUBLIC KEY-----"
KGS_PRIMARY_RSA_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----\nXYZ\n-----END RSA PRIVATE KEY-----"
`
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	environment, err := LoadEnvironment(envPath)
	if err != nil {
		t.Fatalf("LoadEnvironment() error = %v", err)
	}

	account, ok := environment.Profiles["kgs_primary"]
	if !ok {
		t.Fatal("expected kgs_primary profile to be loaded")
	}
	if got, want := account.TerminalCode, "12345"; got != want {
		t.Fatalf("TerminalCode = %q, want %q", got, want)
	}
	if got, want := account.PublicKeyValue(), "-----BEGIN PUBLIC KEY-----\nABC\n-----END PUBLIC KEY-----"; got != want {
		t.Fatalf("PublicKeyValue() = %q, want %q", got, want)
	}
	if got, want := account.PrivateKeyValue(), "-----BEGIN RSA PRIVATE KEY-----\nXYZ\n-----END RSA PRIVATE KEY-----"; got != want {
		t.Fatalf("PrivateKeyValue() = %q, want %q", got, want)
	}
	if got, want := environment.ProfileMap["elqr_primary"], "kgs_primary"; got != want {
		t.Fatalf("ProfileMap[elqr_primary] = %q, want %q", got, want)
	}
}

func TestLoadEnvironmentIgnoresMissingEnvFile(t *testing.T) {
	t.Parallel()

	environment, err := LoadEnvironment(filepath.Join(t.TempDir(), ".missing.env"))
	if err != nil {
		t.Fatalf("LoadEnvironment() error = %v", err)
	}
	if environment.BaseURL == "" {
		t.Fatal("expected default base url for missing env file")
	}
}
