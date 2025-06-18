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

type Flag string

const (
	FlagSYN Flag = "SYN" // SYN flag for TCP connections
	FlagFIN Flag = "FIN" // FIN flag for TCP connections
	FlagRST Flag = "RST" // RST flag for TCP connections
	FlagACK Flag = "ACK" // ACK flag for TCP connections
	FlagPSH Flag = "PSH" // PSH flag for TCP connections
	FlagURG Flag = "URG" // URG flag for TCP connections
)

type Packet struct {
	Key  ClientKey // unique key for the connection
	Flag Flag      // TCP flags (SYN, FIN, RST)
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

func GeneratePacket(key ClientKey, flag Flag, size int) Packet {
	return Packet{
		Key:  key,
		Flag: flag,
		Size: size,
	}
}

func newLoadBalancer(mode LBMode, algo algorithms.Algorithm) *LoadBalancer {
	return &LoadBalancer{
		Mode:      mode,
		servers:   make([]*server.Server, 0),
		connTable: make(map[ClientKey]*server.Server),
		algo:      algo,
	}
}

func (lb *LoadBalancer) AddServer(srv *server.Server) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.servers = append(lb.servers, srv)
}

func (lb *LoadBalancer) RemoveServer(srv *server.Server) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for i, s := range lb.servers {
		if s == srv {
			lb.servers = append(lb.servers[:i], lb.servers[i+1:]...)
			for key, value := range lb.connTable {
				if value == srv {
					delete(lb.connTable, key)
				}
			}

			break
		}
	}
}

func (lb *LoadBalancer) GetServers() []*server.Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.servers
}
