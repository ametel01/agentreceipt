package buildinfo

import "testing"

func TestName(t *testing.T) {
	t.Parallel()

	if got := Name(); got != "agentreceipt" {
		t.Fatalf("Name() = %q, want %q", got, "agentreceipt")
	}
}
