package httpserver

import (
	"encoding/json"
	"fmt"
)

type Handler func(json.RawMessage) error

type Protocol struct {
	handlers map[CommandType]Handler
}

func NewProtocol() *Protocol {
	return &Protocol{handlers: make(map[CommandType]Handler)}
}

func Register[T any](p *Protocol, msgType CommandType, fn func(T) error) {
	p.handlers[msgType] = func(raw json.RawMessage) error {
		var msg T
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("Unmarshalling was impossible: %v", err)
		}
		return fn(msg)
	}
}
