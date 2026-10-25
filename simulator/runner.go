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
	wallStart := time.Now()
	execution := world.Info()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		next := world.Now()
		remaining := world.EndTime() - next
		if remaining > execution.SnapshotInterval {
			remaining = execution.SnapshotInterval
		}
		next += remaining
		if execution.Speed > 0 {
			pacedTime := time.Duration(float64(next) / execution.Speed)
			deadline := wallStart.Add(pacedTime)
			if delay := time.Until(deadline); delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}

		// Preserve admission order and decode inside each zero-delay DES callback.
		// A command's publication remains separate from the end-of-step publication.
		for n := len(commands); n > 0; n-- {
			command := <-commands
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
		err := world.Advance(ctx, next)
		world.SampleTraffic()
		if err != nil {
			return err
		}
		if publish != nil {
			if err := publish(); err != nil {
				return err
			}
		}
		if next == world.EndTime() {
			return nil
		}
	}
}
