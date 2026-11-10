package testsupport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/scenario"
)

func AssertCLIManifest(
	t *testing.T,
	encoded json.RawMessage,
	input scenario.Scenario,
	traceEnabled bool,
) {
	t.Helper()
	assertManifest(t, encoded, input, traceEnabled, nil)
}

// AssertInProcessManifest accounts for the library test binary's build identity.
// Reports from the real executable still require a nonempty module version.
func AssertInProcessManifest(
	t *testing.T,
	encoded json.RawMessage,
	input scenario.Scenario,
	traceEnabled bool,
) {
	t.Helper()
	build, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("test binary has no build identity")
	}
	assertManifest(t, encoded, input, traceEnabled, &build.Main.Version)
}

func assertManifest(
	t *testing.T,
	encoded json.RawMessage,
	input scenario.Scenario,
	traceEnabled bool,
	testModuleVersion *string,
) {
	t.Helper()
	var manifest struct {
		Model          string          `json:"model"`
		Scenario       json.RawMessage `json:"scenario"`
		ScenarioSHA256 string          `json:"scenarioSha256"`
		Execution      struct {
			Headless     bool    `json:"headless"`
			Speed        float64 `json:"speed"`
			TraceEnabled bool    `json:"traceEnabled"`
		} `json:"execution"`
		Build struct {
			GoVersion     string `json:"goVersion"`
			ModuleVersion string `json:"moduleVersion"`
			GOOS          string `json:"goos"`
			GOARCH        string `json:"goarch"`
		} `json:"build"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Model != "host-cpu-v1" {
		t.Fatalf("model profile = %q", manifest.Model)
	}
	if !manifest.Execution.Headless || manifest.Execution.Speed != 0 ||
		manifest.Execution.TraceEnabled != traceEnabled {
		t.Fatalf("actual execution settings = %+v", manifest.Execution)
	}
	if manifest.Build.GoVersion == "" ||
		manifest.Build.GOOS != runtime.GOOS || manifest.Build.GOARCH != runtime.GOARCH {
		t.Fatalf("missing or incorrect build identity: %+v", manifest.Build)
	}
	if testModuleVersion == nil && manifest.Build.ModuleVersion == "" {
		t.Fatal("executable report has no module version")
	}
	if testModuleVersion != nil && manifest.Build.ModuleVersion != *testModuleVersion {
		t.Fatalf("module version = %q, want test binary version %q", manifest.Build.ModuleVersion, *testModuleVersion)
	}

	// The archived input retains requested playback and exact large integers.
	canonical, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, manifest.Scenario); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compact.Bytes(), canonical) {
		t.Fatal("manifest scenario differs from the input document")
	}
	checksum := sha256.Sum256(canonical)
	if manifest.ScenarioSHA256 != hex.EncodeToString(checksum[:]) {
		t.Fatalf("manifest checksum = %q", manifest.ScenarioSHA256)
	}
	restored, err := scenario.DecodeDocument(manifest.Scenario)
	if err != nil {
		t.Fatalf("stored scenario cannot be replayed: %v", err)
	}
	if restored.Fixed.Execution.Seed != input.Fixed.Execution.Seed ||
		restored.Fixed.Execution.Speed != input.Fixed.Execution.Speed {
		t.Fatal("stored scenario lost the original seed or playback settings")
	}
}

func AssertReportVersions(t *testing.T, encoded []byte) {
	t.Helper()
	var envelope struct {
		ReportVersion int             `json:"reportVersion"`
		LegacyVersion json.RawMessage `json:"version"`
		Manifest      struct {
			Scenario struct {
				ScenarioVersion int             `json:"scenarioVersion"`
				LegacyVersion   json.RawMessage `json:"version"`
			} `json:"scenario"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("invalid report envelope: %v", err)
	}
	if envelope.ReportVersion != 1 || envelope.LegacyVersion != nil {
		t.Fatal("report must use reportVersion: 1 without an unscoped version field")
	}
	definition := envelope.Manifest.Scenario
	if definition.ScenarioVersion != 2 || definition.LegacyVersion != nil {
		t.Fatal("archived scenario must use scenarioVersion: 2 without an unscoped version field")
	}
}

func AssertCLIRejected(t *testing.T, output CLIOutput) {
	t.Helper()
	var exit *exec.ExitError
	if !errors.As(output.Err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("invalid request returned %v, want a nonzero exit", output.Err)
	}
	if len(bytes.TrimSpace(output.Stdout)) != 0 {
		t.Fatalf("rejected request printed a report: %s", output.Stdout)
	}
	if strings.TrimSpace(output.Stderr) == "" {
		t.Fatal("rejected request returned no explanation")
	}
}

func BuildCLI(t *testing.T) string {
	t.Helper()
	name := "simulator"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", path, ".")
	// CLI callers live in internal/app and internal/live; build from the module root.
	command.Dir = filepath.Join("..", "..")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return path
}

func ExecuteCLI(
	t *testing.T,
	executable string,
	arguments ...string,
) CLIOutput {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI did not finish an unpaced run: %v\n%s", ctx.Err(), stderr.String())
	}
	return CLIOutput{
		Stdout: stdout.Bytes(),
		Stderr: stderr.String(),
		Err:    err,
	}
}

func WriteCLIInput(t *testing.T, input scenario.Scenario) string {
	t.Helper()
	encoded, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func DecodeCLIReport(
	t *testing.T,
	output CLIOutput,
	status string,
) CLIReport {
	t.Helper()
	if status == "finished" && output.Err != nil {
		t.Fatalf("run failed: %v\n%s", output.Err, output.Stderr)
	}
	if status == "failed" {
		var exit *exec.ExitError
		if !errors.As(output.Err, &exit) || exit.ExitCode() == 0 {
			t.Fatalf("failed run returned %v, want a nonzero process exit", output.Err)
		}
	}
	var report CLIReport
	AssertReportVersions(t, output.Stdout)
	if err := json.Unmarshal(output.Stdout, &report); err != nil {
		t.Fatalf("invalid CLI report: %v\n%s\n%s", err, output.Stdout, output.Stderr)
	}
	if report.ReportVersion != 1 || report.Status != status || len(report.Manifest) == 0 {
		t.Fatalf("report header = %+v", report)
	}
	return report
}

type CLIOutput struct {
	Stdout []byte
	Stderr string
	Err    error
}

type CLIReport struct {
	ReportVersion int                       `json:"reportVersion"`
	Status        string                    `json:"status"`
	Error         string                    `json:"error"`
	Manifest      json.RawMessage           `json:"manifest"`
	Result        contract.SimulationResult `json:"result"`
}
