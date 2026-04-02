package paymentsgate

import "testing"

func TestFlattenForSignatureMatchesWebhookV3Rules(t *testing.T) {
	t.Parallel()

	body := []byte(`{"status":"paid","deal":{"id":"123","steps":[true,false],"amount":100}}`)

	flat, err := FlattenForSignature(body)
	if err != nil {
		t.Fatalf("FlattenForSignature() error = %v", err)
	}
	if got, want := flat, "truefalse100123paid"; got != want {
		t.Fatalf("FlattenForSignature() = %q, want %q", got, want)
	}

	checksum, err := ChecksumSHA256(body)
	if err != nil {
		t.Fatalf("ChecksumSHA256() error = %v", err)
	}
	if got, want := checksum, "93d3033fcf539b3061b210baa770f40adb9111a2d00036458061bdf14600c9c0"; got != want {
		t.Fatalf("ChecksumSHA256() = %q, want %q", got, want)
	}
}

func TestNaturalLessTreatsNumbersNaturally(t *testing.T) {
	t.Parallel()

	if !naturalLess("field_2", "field_10") {
		t.Fatal("expected field_2 to sort before field_10")
	}
}
