package presentation

import "testing"

// A scenario's effective MSS is retained in the exported result.
func TestResultReportsManualMSS(t *testing.T) {
	definition := projectionScenario()
	mssBytes := 1460
	definition.Fixed.TCP.MSSBytes = &mssBytes
	world := projectionWorld(t, definition)
	advanceProjection(t, world, world.EndTime())

	if got := BuildResult(world, 0).TCP.MSSBytes; got != mssBytes {
		t.Fatalf("reported MSS=%d, want %d", got, mssBytes)
	}
}
