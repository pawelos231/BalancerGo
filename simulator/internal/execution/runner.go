// Package execution runs simulations and records experiment metadata.
package execution

import (
	"context"
	"time"

	"load-balancer/internal/simulation"
)

// RunWorld owns real-time pacing and cancellation. World owns all
// virtual events; publishing observes it synchronously on this same goroutine.
func RunWorld(
	ctx context.Context,
	world *simulation.World,
	publish func() error,
) error {
	return runWorldWithPacing(ctx, world, publish, false)
}

// Headless execution changes pacing without editing the admitted configuration.
func RunUnpacedWorld(ctx context.Context, world *simulation.World) error {
	return runWorldWithPacing(ctx, world, nil, true)
}

func runWorldWithPacing(
	ctx context.Context,
	world *simulation.World,
	publish func() error,
	unpaced bool,
) error {
	wallStart := time.Now()
	execution := world.Info()
	if unpaced {
		execution.Speed = 0
	}
	nextSnapshot := wallStart.Add(execution.SnapshotInterval)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Unpaced runs can reach the end immediately. Playback only allows
		// virtual time already covered by elapsed real time and the chosen speed.
		until := world.EndTime()
		if execution.Speed > 0 {
			allowed := float64(time.Since(wallStart)) * execution.Speed
			if allowed < float64(until) {
				until = time.Duration(allowed)
			}
		}

		reached, err := world.AdvanceBatch(ctx, until)
		if err != nil {
			world.SampleTraffic()
			return err
		}
		finished := reached && until == world.EndTime()
		snapshotDue := publish != nil && !time.Now().Before(nextSnapshot)
		if finished || snapshotDue {
			world.SampleTraffic()
		}
		if publish != nil && (finished || snapshotDue) {
			if err := publish(); err != nil {
				return err
			}
			// Slow publication does not build a backlog of missed refreshes.
			nextSnapshot = time.Now().Add(execution.SnapshotInterval)
		}
		if finished {
			return nil
		}
		if !reached || execution.Speed == 0 {
			continue
		}

		// Playback waits at most 10 real ms before checking progress again.
		// Stop wakes it sooner, even with a very long UI interval.
		delay := 10 * time.Millisecond
		remaining := float64(world.EndTime()-world.Now()) / execution.Speed
		if remaining < float64(delay) {
			delay = time.Duration(remaining)
		}
		if publish != nil {
			delay = min(delay, time.Until(nextSnapshot))
		}
		timer := time.NewTimer(max(0, delay))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
