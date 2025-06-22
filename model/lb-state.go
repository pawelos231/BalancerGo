package model

type LoadBalancerState struct {
	Mode              LBMode
	Algorithm         string
	ServersAndClients map[ServerState][]ClientState
}
