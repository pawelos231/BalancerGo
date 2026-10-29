package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	httpserver "load-balancer/http"
	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
)

// submitFrontendCommand translates a wire action without decoding its runtime
// payload early. The application still reports those errors at the DES boundary.
func submitFrontendCommand(control *runController, ctx context.Context, input contract.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var kind commandKind
	switch input.Type {
	case contract.StartSimulation:
		kind = startCommand
	case contract.StopSimulation:
		kind = stopCommand
	case contract.SetAlgorithm:
		kind = algorithmCommand
	case contract.SpawnClient:
		kind = spawnCommand
	default:
		return fmt.Errorf("unsupported command: %s", input.Type)
	}
	return control.submit(ctx, runtimeCommand{kind: kind, Payload: input.Payload})
}

// applyRuntimeCommand runs on the owner, within the scheduled command event.
// Decoded frontend messages are reduced to typed World operations here.
func applyRuntimeCommand(world *simulation.World, command runtimeCommand) error {
	switch command.kind {
	case algorithmCommand:
		var message contract.SetAlgorithmMsg
		if err := json.Unmarshal(command.Payload, &message); err != nil {
			return err
		}
		return world.SetAlgorithm(message.Name)
	case spawnCommand:
		var message contract.SpawnClientMsg
		if err := json.Unmarshal(command.Payload, &message); err != nil {
			return err
		}
		return world.SpawnClients(message.Count, message.GroupID)
	default:
		return fmt.Errorf("unsupported command: %s", commandName(command.kind))
	}
}

func commandName(kind commandKind) contract.CommandType {
	switch kind {
	case startCommand:
		return contract.StartSimulation
	case stopCommand:
		return contract.StopSimulation
	case algorithmCommand:
		return contract.SetAlgorithm
	case spawnCommand:
		return contract.SpawnClient
	default:
		return ""
	}
}

// admissionError preserves the public HTTP status mapping for application errors.
func admissionError(err error) error {
	switch {
	case errors.Is(err, errSimulationBusy):
		return httpserver.ErrSimulationBusy
	case errors.Is(err, errCommandQueueFull):
		return httpserver.ErrCommandQueueFull
	case errors.Is(err, errServiceClosed):
		return httpserver.ErrServiceClosed
	default:
		return err
	}
}

// publicConfig converts CLI defaults into the existing frontend settings.
func (c configuration) publicConfig() contract.SimulationConfig {
	receiveBufferBytes := c.receiveBufferBytes
	if receiveBufferBytes == 0 {
		receiveBufferBytes = model.DefaultReceiveBufferBytes
	}

	return contract.SimulationConfig{
		Clients:                 c.clients,
		Servers:                 c.servers,
		Packets:                 c.packets,
		Bytes:                   c.streamBytes,
		ReceiveBufferBytes:      receiveBufferBytes,
		DurationSeconds:         float64(c.duration) / float64(time.Second),
		ClientGapMs:             float64(c.clientGap) / float64(time.Millisecond),
		PacketGapMs:             float64(c.packetGap) / float64(time.Millisecond),
		SnapshotIntervalMs:      float64(c.snapshotInterval) / float64(time.Millisecond),
		BandwidthBitsPerSecond:  c.bandwidth,
		ForwardingBitsPerSecond: c.forwardingBitsPerSecond,
		PacketTravelTimeMs:      float64(c.packetTravelTime) / float64(time.Millisecond),
		LinkQueue:               c.queue,
		Loss:                    c.loss,

		IngressBandwidthBitsPerSecond: c.ingress.BandwidthBitsPerSecond,
		IngressPacketTravelTimeMs:     float64(c.ingress.PacketTravelTime) / float64(time.Millisecond),
		IngressQueueCapacity:          c.ingress.QueueCapacity,
		IngressLoss:                   c.ingress.LossRate,

		Seed:      c.seed,
		Algorithm: c.algorithm,
		Speed:     c.speed,
		MaxEvents: c.maxEvents,
	}
}

// configurationFromPublic overlays experiment fields while retaining process
// options. Callers can decode omitted JSON fields into base.publicConfig().
func configurationFromPublic(c contract.SimulationConfig, base configuration) (configuration, error) {
	if c.ReceiveBufferBytes < 1 || c.ReceiveBufferBytes > math.MaxUint16 {
		return configuration{}, errors.New("receiveBufferBytes must be in 1..65535")
	}

	base.clients, base.servers = c.Clients, c.Servers
	base.packets, base.streamBytes = c.Packets, c.Bytes
	base.receiveBufferBytes = c.ReceiveBufferBytes
	base.bandwidth, base.queue = c.BandwidthBitsPerSecond, c.LinkQueue
	base.forwardingBitsPerSecond = c.ForwardingBitsPerSecond
	base.loss, base.seed = c.Loss, c.Seed
	base.algorithm, base.speed, base.maxEvents = c.Algorithm, c.Speed, c.MaxEvents
	base.ingress.BandwidthBitsPerSecond = c.IngressBandwidthBitsPerSecond
	base.ingress.QueueCapacity = c.IngressQueueCapacity
	base.ingress.LossRate = c.IngressLoss
	for _, field := range []struct {
		name  string
		value float64
		unit  time.Duration
		out   *time.Duration
	}{
		{"durationSeconds", c.DurationSeconds, time.Second, &base.duration},
		{"clientGapMs", c.ClientGapMs, time.Millisecond, &base.clientGap},
		{"packetGapMs", c.PacketGapMs, time.Millisecond, &base.packetGap},
		{"snapshotIntervalMs", c.SnapshotIntervalMs, time.Millisecond, &base.snapshotInterval},
		{"packetTravelTimeMs", c.PacketTravelTimeMs, time.Millisecond, &base.packetTravelTime},
		{
			name:  "ingressPacketTravelTimeMs",
			value: c.IngressPacketTravelTimeMs,
			unit:  time.Millisecond,
			out:   &base.ingress.PacketTravelTime,
		},
	} {
		duration, err := configDuration(field.name, field.value, field.unit)
		if err != nil {
			return configuration{}, err
		}
		*field.out = duration
	}
	return base.normalized()
}

// Convert only after checking the floating-point input and scaled value.
// float64(MaxInt64) rounds to 2^63, so equality is already outside the safe
// conversion range. Positive fractions of a nanosecond truncate to zero.
func configDuration(name string, value float64, unit time.Duration) (time.Duration, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("%s must be finite and nonnegative", name)
	}
	nanoseconds := value * float64(unit)
	if math.IsInf(nanoseconds, 0) || nanoseconds >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("%s exceeds time.Duration range", name)
	}
	return time.Duration(nanoseconds), nil
}
