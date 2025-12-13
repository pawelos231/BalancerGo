package httpserver

import (
	"encoding/json"
	"fmt"
)

type Handler func(json.RawMessage) error

type Protocol struct {
	handlers map[string]Handler
}

func NewProtocol() *Protocol {
	return &Protocol{handlers: make(map[string]Handler)}
}

func Register[T any](p *Protocol, msgType string, fn func(T) error) {
	p.handlers[msgType] = func(raw json.RawMessage) error {
		var msg T
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("Unmarshalling was impossible: %v", err)
		}
		return fn(msg)
	}
}
