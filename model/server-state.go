package model

type ServerState struct {
	ID                     string
	Name                   string
	Address                string
	Port                   int16
	Active                 bool
	ActiveConnections      int32
	HalfOpenConnections    int32
	LatencyP95Milli        int32
	NumberOfHandledPackets int32
}
