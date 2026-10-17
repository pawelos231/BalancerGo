package model

type ServerStateWithClients struct {
	Server  ServerState   `json:"server"`
	Clients []ClientState `json:"clients"`
}
