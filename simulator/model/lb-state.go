package model

type LoadBalancerState struct {
	Mode      LBMode                   `json:"mode"`
	Algorithm string                   `json:"algorithm"`
	Servers   []ServerStateWithClients `json:"servers"`
}
