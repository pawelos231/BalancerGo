package model

type ServerState struct {
	ID                     string
	Name                   string
	Address                string
	Port                   uint16
	Active                 bool
	ActiveConnections      uint32
	HalfOpenConnections    uint32
	LatencyP95Milli        uint32
	NumberOfHandledPackets uint32
	ReceivedPayloadBytes   uint64
	CpuUsage               uint32 // Packets received in the last statistics window.
}
