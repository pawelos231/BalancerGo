package live

import (
	"testing"

	"load-balancer/internal/testsupport"
)

// The browser summary retains an explicitly configured native scenario MSS.
func TestScenarioSummaryPreservesManualMSS(t *testing.T) {
	input := testsupport.Scenario()
	input.Fixed.TCP.MSSBytes = testsupport.Pointer(1460)
	if scenarioSummary(testsupport.Compile(t, input)).MSSBytes != 1460 {
		t.Fatal("MSS was lost between scenario and browser summary")
	}
}
