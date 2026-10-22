package network

// Ingress merges separate input FIFOs onto one physical incoming link.
// Its queues and link belong to the engine's single owner.
type Ingress struct {
	link         *Link
	inputs       []*Input
	nextSequence uint64
	stats        Stats

	transmittingInput *Input
	transmittingBytes int
	lossesBefore      uint64
}

// Input holds one source class's waiting packets, without its own transmitter.
// Each input can use the full shared link when the other inputs are idle.
type Input struct {
	ingress   *Ingress
	queue     []ingressPacket
	queueHead int
	stats     Stats
}

// ingressPacket preserves a packet's callback and its global admission order.
type ingressPacket struct {
	sizeBytes int
	onArrival func()
	sequence  uint64
}

// NewIngress takes exclusive ownership of a fresh physical link.
func NewIngress(link *Link) *Ingress {
	if link == nil {
		panic("network: ingress link must not be nil")
	}

	ingress := &Ingress{link: link}
	link.useExternalQueue(ingress.finishTransmission)
	return ingress
}

// AddInput creates one FIFO with the physical configuration's waiting capacity.
// Active transmissions and travelling packets do not occupy waiting slots.
func (ingress *Ingress) AddInput() *Input {
	input := &Input{ingress: ingress}
	ingress.inputs = append(ingress.inputs, input)
	return input
}

// Config returns the shared link's limits; QueueCapacity applies to each input.
func (ingress *Ingress) Config() Config {
	return ingress.link.Config()
}

// Transmit admits a packet or drops it when this input's waiting FIFO is full.
// The callback may run later or remain pending when the experiment ends.
func (input *Input) Transmit(sizeBytes int, onArrival func()) bool {
	if sizeBytes < 0 {
		panic("network: packet size must not be negative")
	}
	if onArrival == nil {
		panic("network: arrival callback must not be nil")
	}

	ingress := input.ingress
	// Validate transmission duration before changing counters or accepting the packet.
	serializationDuration(sizeBytes, ingress.link.config.BandwidthBitsPerSecond)
	canStartNow := ingress.link.Ready()
	queueFull := input.queued() >= ingress.link.config.QueueCapacity
	accepted := canStartNow || !queueFull
	if accepted && ingress.nextSequence == ^uint64(0) {
		panic("network: ingress enqueue sequence overflow")
	}

	input.stats.OfferedPackets++
	input.stats.OfferedBytes += uint64(sizeBytes)
	ingress.stats.OfferedPackets++
	ingress.stats.OfferedBytes += uint64(sizeBytes)
	if !accepted {
		input.stats.QueueDroppedPackets++
		input.stats.QueueDroppedBytes += uint64(sizeBytes)
		ingress.stats.QueueDroppedPackets++
		ingress.stats.QueueDroppedBytes += uint64(sizeBytes)
		return false
	}

	input.stats.AcceptedPackets++
	input.stats.AcceptedBytes += uint64(sizeBytes)
	ingress.stats.AcceptedPackets++
	ingress.stats.AcceptedBytes += uint64(sizeBytes)
	ingress.stats.QueuedPackets++
	ingress.nextSequence++
	input.queue = append(input.queue, ingressPacket{
		sizeBytes: sizeBytes,
		onArrival: onArrival,
		sequence:  ingress.nextSequence,
	})

	// Immediate dispatch keeps zero-capacity admission and link event order intact.
	ingress.dispatch()
	return true
}

// Stats combines all FIFO admissions with progress on the one physical link.
func (ingress *Ingress) Stats() Stats {
	stats := ingress.link.Stats()
	stats.OfferedPackets = ingress.stats.OfferedPackets
	stats.OfferedBytes = ingress.stats.OfferedBytes
	stats.AcceptedPackets = ingress.stats.AcceptedPackets
	stats.AcceptedBytes = ingress.stats.AcceptedBytes
	stats.QueueDroppedPackets = ingress.stats.QueueDroppedPackets
	stats.QueueDroppedBytes = ingress.stats.QueueDroppedBytes
	stats.QueuedPackets = ingress.stats.QueuedPackets
	return stats
}

// Stats reports this input's admissions, FIFO and physical packet outcomes.
func (input *Input) Stats() Stats {
	stats := input.stats
	stats.QueuedPackets = input.queued()
	return stats
}

// queued excludes consumed slots retained at the front of the slice.
func (input *Input) queued() int {
	return len(input.queue) - input.queueHead
}

// dispatch starts the oldest FIFO head when the shared transmitter is free.
func (ingress *Ingress) dispatch() {
	if !ingress.link.Ready() {
		return
	}

	var oldest *Input
	for _, input := range ingress.inputs {
		if input.queued() == 0 {
			continue
		}
		if oldest == nil {
			oldest = input
			continue
		}

		candidateSequence := input.queue[input.queueHead].sequence
		oldestSequence := oldest.queue[oldest.queueHead].sequence
		if candidateSequence < oldestSequence {
			oldest = input
		}
	}
	if oldest == nil {
		return
	}

	packet := oldest.queue[oldest.queueHead]
	ingress.transmittingInput = oldest
	ingress.transmittingBytes = packet.sizeBytes
	ingress.lossesBefore = ingress.link.stats.LostPackets
	oldest.stats.Transmitting = true

	started := ingress.link.TryStart(packet.sizeBytes, func() {
		oldest.stats.TravellingPackets--
		oldest.stats.DeliveredPackets++
		oldest.stats.DeliveredBytes += uint64(packet.sizeBytes)
		packet.onArrival()
	})
	if !started {
		panic("network: ready ingress could not start its transmission")
	}

	oldest.removeHead()
	ingress.stats.QueuedPackets--
}

// finishTransmission attributes loss before another packet starts on the link.
func (ingress *Ingress) finishTransmission() {
	input := ingress.transmittingInput
	input.stats.Transmitting = false
	if ingress.link.stats.LostPackets != ingress.lossesBefore {
		input.stats.LostPackets++
		input.stats.LostBytes += uint64(ingress.transmittingBytes)
	} else {
		// Link schedules arrival before notifying us, even when travel time is zero.
		input.stats.TravellingPackets++
	}

	ingress.transmittingInput = nil
	ingress.transmittingBytes = 0
	ingress.dispatch()
}

// removeHead releases captured packet state and occasionally compacts the FIFO.
func (input *Input) removeHead() {
	const minConsumedSlots = 1024

	input.queue[input.queueHead] = ingressPacket{}
	input.queueHead++
	remaining := input.queued()
	if remaining == 0 {
		input.queue = input.queue[:0]
		input.queueHead = 0
		return
	}

	enoughConsumed := input.queueHead >= minConsumedSlots
	atLeastHalfConsumed := input.queueHead >= remaining
	if enoughConsumed && atLeastHalfConsumed {
		copy(input.queue, input.queue[input.queueHead:])
		clear(input.queue[remaining:])
		input.queue = input.queue[:remaining]
		input.queueHead = 0
	}
}
