package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"runtime/debug"
	"strconv"

	"load-balancer/internal/contract"
)

const experimentModel = "host-cpu-v1"

// Context hashes the canonical input once, before the run starts.
// Execution describes actual playback; the input still keeps the requested settings.
func Context(
	document []byte,
	execution contract.ExperimentExecution,
) contract.ExperimentContext {
	hash := sha256.Sum256(document)
	return contract.ExperimentContext{
		Model:          experimentModel,
		ScenarioSHA256: hex.EncodeToString(hash[:]),
		Execution:      execution,
		Build:          experimentBuild(),
	}
}

// Manifest preserves the CLI format while sharing metadata with live runs.
func Manifest(document []byte, traced bool) contract.ExperimentManifest {
	return contract.ExperimentManifest{
		ExperimentContext: Context(document, contract.ExperimentExecution{
			Headless:     true,
			Speed:        0,
			TraceEnabled: traced,
		}),
		Scenario: document,
	}
}

func experimentBuild() contract.ExperimentBuild {
	build := contract.ExperimentBuild{
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return build
	}
	build.ModuleVersion = info.Main.Version
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			build.VCSRevision = setting.Value
		case "vcs.modified":
			modified, err := strconv.ParseBool(setting.Value)
			if err == nil {
				build.VCSModified = &modified
			}
		}
	}
	return build
}
