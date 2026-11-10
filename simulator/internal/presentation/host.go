package presentation

import (
	"time"

	"load-balancer/host"
	"load-balancer/internal/contract"
)

// hostState records virtual CPU counters and the effective enabled/disabled profile.
func hostState(state host.Stats) *contract.HostState {
	meanWait := 0.0
	if state.StartedPackets > 0 {
		meanWait = state.QueueWaitTotal.Seconds() / float64(state.StartedPackets)
	}
	return &contract.HostState{
		Enabled:                         state.Enabled,
		Cores:                           state.Cores,
		PacketProcessingTimeNanoseconds: state.PacketProcessingTime,
		QueueCapacity:                   state.QueueCapacity,
		BusyCores:                       state.BusyCores,
		QueuedPackets:                   state.QueuedPackets,
		MaxQueuedPackets:                state.MaxQueuedPackets,
		OfferedPackets:                  state.OfferedPackets,
		AcceptedPackets:                 state.AcceptedPackets,
		StartedPackets:                  state.StartedPackets,
		ProcessedPackets:                state.ProcessedPackets,
		QueueDroppedPackets:             state.QueueDroppedPackets,
		DiscardedPackets:                state.DiscardedPackets,
		BusyTimeSeconds:                 state.BusyTime.Seconds(),
		AvailableCoreTimeSeconds:        state.AvailableCoreTime.Seconds(),
		ObservedTimeSeconds:             state.ObservedTime.Seconds(),
		MeanQueueWaitSeconds:            meanWait,
		MaxQueueWaitSeconds:             state.MaxQueueWait.Seconds(),
		Utilization:                     state.Utilization(),
	}
}

// appendHostMetrics retains retiring work and CPU history after a switch to zero cost.
// Sampling reads counters without advancing the model.
func appendHostMetrics(metrics []nodeMetric, state host.Stats) []nodeMetric {
	enabled := 0.0
	if state.Enabled {
		enabled = 1
	}
	metrics = append(metrics, nodeMetric{
		key: "hostCPUEnabled", label: "CPU model enabled (1 = yes)",
		value: decimalMetric(enabled), static: true,
	})
	if !state.Enabled && state.BusyCores == 0 && state.BusyTime == 0 &&
		state.QueuedPackets == 0 && state.MaxQueuedPackets == 0 &&
		state.QueueDroppedPackets == 0 && state.DiscardedPackets == 0 {
		return metrics
	}
	meanWaitMs := 0.0
	if state.StartedPackets > 0 {
		meanWaitMs = float64(state.QueueWaitTotal) /
			(float64(time.Millisecond) * float64(state.StartedPackets))
	}
	return append(metrics,
		nodeMetric{"hostCores", "Virtual CPU cores", "cores", integerMetric(state.Cores), true},
		nodeMetric{"hostPacketProcessingTimeUs", "Processing per packet", "µs", decimalMetric(float64(state.PacketProcessingTime) / float64(time.Microsecond)), true},
		nodeMetric{"hostQueueCapacity", "CPU waiting slots", "packets", integerMetric(state.QueueCapacity), true},
		nodeMetric{"hostBusyCores", "Busy CPU cores now", "cores", integerMetric(state.BusyCores), false},
		nodeMetric{"hostQueuedPackets", "Packets waiting for CPU", "packets", integerMetric(state.QueuedPackets), false},
		nodeMetric{"hostMaxQueuedPackets", "Peak CPU queue", "packets", integerMetric(state.MaxQueuedPackets), false},
		nodeMetric{"hostUtilization", "CPU utilization (virtual run mean)", "ratio", decimalMetric(state.Utilization()), false},
		nodeMetric{
			key: "hostBusyTimeSeconds", label: "Cumulative occupied core time", unit: "s",
			value: decimalMetric(state.BusyTime.Seconds()),
		},
		nodeMetric{
			key: "hostAvailableCoreTimeSeconds", label: "Cumulative available core time", unit: "s",
			value: decimalMetric(state.AvailableCoreTime.Seconds()),
		},
		nodeMetric{"hostQueueDroppedPackets", "CPU queue drops", "packets", integerMetric(state.QueueDroppedPackets), false},
		nodeMetric{"hostProcessedPackets", "Packets processed by CPU", "packets", integerMetric(state.ProcessedPackets), false},
		nodeMetric{"hostMeanQueueWaitMs", "Mean CPU queue wait (started packets)", "ms", decimalMetric(meanWaitMs), false},
		nodeMetric{"hostMaxQueueWaitMs", "Maximum observed CPU queue wait", "ms", decimalMetric(float64(state.MaxQueueWait) / float64(time.Millisecond)), false},
	)
}
