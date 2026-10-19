package model

type ClientKey struct {
	SrcIP    string   // source IP address of the connection
	SrcPort  uint16   // source port of the connection
	DstIP    string   // destination IP address (server)
	DstPort  uint16   // destination port (server)
	Protocol Protocol // protocol used (e.g. "TCP", "UDP")
}
