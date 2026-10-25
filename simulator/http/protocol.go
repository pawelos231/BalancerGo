package httpserver

import (
	"encoding/json"
	"fmt"

	"load-balancer/internal/contract"
)

type Handler func(json.RawMessage) error

type Protocol struct {
	handlers map[contract.CommandType]Handler
}

func NewProtocol() *Protocol {
	return &Protocol{handlers: make(map[contract.CommandType]Handler)}
}

func Register[T any](p *Protocol, msgType contract.CommandType, fn func(T) error) {
	p.handlers[msgType] = func(raw json.RawMessage) error {
		var msg T
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("Unmarshalling was impossible: %v", err)
		}
		return fn(msg)
	}
}
