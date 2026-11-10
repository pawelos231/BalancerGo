package execution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"load-balancer/internal/presentation"
	"load-balancer/internal/simulation"
	"load-balancer/internal/testsupport"
	"load-balancer/scenario"
)

// runnerWorld creates an idle network for application runner tests.
func runnerWorld(t *testing.T, execution scenario.ExecutionConfig) *simulation.World {
	t.Helper()
	input := testsupport.RuntimeScenario()
	input.Fixed.Execution = execution
	{
		initialState := input.States["0"]
		initialState.Workload.ClientGroups[0].Count = 0
		input.States["0"] = initialState
	}
	{
		initialState := input.States["0"]
		initialState.Resources.Backends = nil
		input.States["0"] = initialState
	}
	return testsupport.NewWorld(testsupport.Compile(t, input))
}

// Both playback speeds use 30 real ms between updates, despite covering
// different amounts of virtual time during those updates.
func TestRunnerSnapshotIntervalUsesRealTime(t *testing.T) {
	for _, speed := range []float64{1, 4} {
		t.Run(fmt.Sprintf("speed_%g", speed), func(t *testing.T) {
			const interval = 30 * time.Millisecond
			world := runnerWorld(t, scenario.ExecutionConfig{
				Duration:         time.Duration(speed * float64(160*time.Millisecond)),
				SnapshotInterval: interval, Speed: speed, MaxEvents: 100_000,
			})
			started := time.Now()
			var updates []time.Duration

			publish := func() error {
				if world.Now() < world.EndTime() {
					updates = append(updates, time.Since(started))
				}
				return nil
			}
			if err := RunWorld(context.Background(), world, publish); err != nil {
				t.Fatal(err)
			}

			if len(updates) == 0 || updates[0] < interval {
				t.Fatalf("real publication times = %v, first must follow %s", updates, interval)
			}
			for index := 1; index < len(updates); index++ {
				if updates[index]-updates[index-1] < interval {
					t.Fatalf("refreshes came faster than the real interval: %v", updates)
				}
			}
		})
	}
}

// A slow callback imitates a busy host. UI updates must happen between batches,
// even when the entire virtual experiment spans only one microsecond.
func TestRunnerPublishesDuringDenseUnpacedRun(t *testing.T) {
	world := runnerWorld(t, scenario.ExecutionConfig{
		Duration: time.Microsecond, SnapshotInterval: 10 * time.Millisecond,
		MaxEvents: 100_000,
	})
	for index := 0; index < 5000; index++ {
		world.ScheduleAt(time.Nanosecond, func() {})
	}
	world.ScheduleAt(0, func() { time.Sleep(20 * time.Millisecond) })
	var updates []time.Duration

	err := RunWorld(context.Background(), world, func() error {
		updates = append(updates, world.Now())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) < 2 || updates[0] >= world.EndTime() {
		t.Fatalf("no update before completion: %v", updates)
	}
	if world.EventsExecuted() != 5001 || updates[len(updates)-1] != world.EndTime() {
		t.Fatalf("same-time events or final snapshot were lost: events=%d updates=%v", world.EventsExecuted(), updates)
	}
}

// A long refresh interval must not delay commands or cancellation until the next UI update.
func TestRunnerStopDoesNotWaitForSnapshot(t *testing.T) {
	world := runnerWorld(t, scenario.ExecutionConfig{
		Duration: time.Hour, SnapshotInterval: time.Hour, Speed: 1, MaxEvents: 100_000,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	world.ScheduleAt(0, cancel)
	frames := 0
	err := RunWorld(ctx, world, func() error { frames++; return nil })
	if !errors.Is(err, context.Canceled) || frames != 0 || world.Now() >= world.EndTime() {
		t.Fatalf("Stop waited for the next snapshot: err=%v frames=%d virtualTime=%s", err, frames, world.Now())
	}
}

// Only the presentation cadence changes; the seeded TCP/network results must match.
func TestRunnerRefreshIntervalDoesNotChangeSimulation(t *testing.T) {
	var previous any
	for _, interval := range []time.Duration{time.Nanosecond, time.Hour} {
		input := testsupport.PayloadScenario()
		input.Fixed.Execution.Speed = 0
		input.Fixed.Execution.SnapshotInterval = interval
		world := testsupport.NewWorld(testsupport.Compile(t, input))
		// Force a batch boundary so the short interval can publish during the run.
		for index := 0; index < 5000; index++ {
			world.ScheduleAt(10*time.Millisecond, func() {})
		}
		// Cross a real clock tick even on hosts with coarse time.Now resolution.
		world.ScheduleAt(0, func() { time.Sleep(2 * time.Millisecond) })
		projector := presentation.NewProjector("refresh-comparison")
		frames := 0
		publish := func() error {
			frames++
			_, _, err := presentation.BuildSnapshot(world, projector, presentation.RunState{})
			return err
		}
		if err := RunWorld(context.Background(), world, publish); err != nil {
			t.Fatal(err)
		}
		if interval == time.Nanosecond && frames < 2 || interval == time.Hour && frames != 1 {
			t.Fatalf("interval %s produced %d snapshots", interval, frames)
		}
		result := presentation.BuildResult(world, 0)
		if previous != nil && !reflect.DeepEqual(previous, result) {
			t.Fatal("refresh interval changed the model's final result")
		}
		previous = result
	}
}
