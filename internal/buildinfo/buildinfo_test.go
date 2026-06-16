package buildinfo

import "testing"

func TestName(t *testing.T) {
	t.Parallel()

	if got := Name(); got != "agentreceipt" {
		t.Fatalf("Name() = %q, want %q", got, "agentreceipt")
	}
}

func TestVersionDefault(t *testing.T) {
	t.Parallel()

	if got := Version(); got == "" {
		t.Fatal("Version() returned an empty string")
	}
}
