package e2e

import (
	"os"
	"testing"
)

func TestPhase1Smoke(t *testing.T) {
	if os.Getenv("CODA_E2E") != "1" {
		t.Skip("set CODA_E2E=1 to run kind e2e harness")
	}
	// Full kind+helm flow is executed by hack/e2e-kind.sh; this test asserts env wiring.
	if os.Getenv("CODA_FAKE_HEALTH") != "1" {
		t.Fatalf("expected CODA_FAKE_HEALTH=1 in e2e environment")
	}
}
