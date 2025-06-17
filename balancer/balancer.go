package balancer

import (
	"load-balancer/algorithms"
	"load-balancer/server"
	"sync"
)

type LBMode string
type Protocol string

const (
	ProtocolTCP Protocol = "TCP" // for now only TCP is supported
)

const (
	LBModeNAT  LBMode = "NAT"
	LBModeSNAT LBMode = "SNAT"
	LBModeDSR  LBMode = "DSR"
)

type ClientKey struct {
	SrcIP    string   // source IP address of the connection
	SrcPort  int16    // source port of the connection
	DstIP    string   // destination IP address (server)
	DstPort  int16    // destination port (server)
	Protocol Protocol // protocol used (e.g. "TCP", "UDP")
}

type Packet struct {
	Key  ClientKey // unique key for the connection
	Syn  bool      // SYN flag set for TCP connections
	Fin  bool      // FIN flag set for TCP connections
	Rst  bool      // RST flag set for TCP connections
	Size int       // size of the packet in bytes
}

type LoadBalancer struct {
	mu        sync.Mutex                   // mutex to protect shared state
	Mode      LBMode                       // NAT / SNAT / DSR
	servers   []*server.Server             // list of registered servers
	connTable map[ClientKey]*server.Server // connection table mapping client keys to servers
	algo      algorithms.Algorithm         // load balancing algorithm to use ex: "RoundRobin", "LeastConnections", etc.
}

func GenerateClientKey(srcIP string, srcPort int16, dstIP string, dstPort int16, protocol Protocol) ClientKey {
	return ClientKey{
		SrcIP:    srcIP,
		SrcPort:  srcPort,
		DstIP:    dstIP,
		DstPort:  dstPort,
		Protocol: protocol,
	}
}

func GeneratePacket(key ClientKey, syn, fin, rst bool, size int) Packet {
	return Packet{
		Key:  key,
		Syn:  syn,
		Fin:  fin,
		Rst:  rst,
		Size: size,
	}
}
