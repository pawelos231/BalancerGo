package main

import (
	"context"
	"log"
	"time"

	"load-balancer/internal/simulation"
)

// runWorld owns real-time pacing and application requests. World owns all
// virtual events; publishing observes it synchronously on this same goroutine.
func runWorld(
	ctx context.Context,
	world *simulation.World,
	commands <-chan runtimeCommand,
	publish func() error,
	handled func(error),
) error {
	return runWorldWithPacing(ctx, world, commands, publish, handled, false)
}

// Headless execution changes pacing without editing the admitted configuration.
func runUnpacedWorld(ctx context.Context, world *simulation.World) error {
	return runWorldWithPacing(ctx, world, nil, nil, nil, true)
}

func runWorldWithPacing(
	ctx context.Context,
	world *simulation.World,
	commands <-chan runtimeCommand,
	publish func() error,
	handled func(error),
	unpaced bool,
) error {
	wallStart := time.Now()
	execution := world.Info()
	if unpaced {
		execution.Speed = 0
	}
	nextSnapshot := wallStart.Add(execution.SnapshotInterval)

	// Commands join the DES at its current time, independently of UI refreshes.
	scheduleCommand := func(command runtimeCommand) {
		world.ScheduleAfter(0, func() {
			err := applyRuntimeCommand(world, command)
			if err != nil {
				log.Printf("command at %s: %v", world.Now(), err)
			} else {
				log.Printf("command at %s: %s %s", world.Now(), commandName(command.kind), command.Payload)
			}
			if handled != nil {
				handled(err)
			}
		})
	}

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

		for n := len(commands); n > 0; n-- {
			scheduleCommand(<-commands)
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
		// A command or Stop wakes it sooner, even with a very long UI interval.
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
		case command := <-commands:
			timer.Stop()
			scheduleCommand(command)
		case <-timer.C:
		}
	}
}
