package main

import (
	"encoding/json"
	"load-balancer/model"
)

// publishedTopology is an immutable JSON document shared with HTTP readers.
// Its separate atomic cache also lets Stop publish while holding the control lock.
type publishedTopology struct {
	document json.RawMessage
}

// publishTopology encodes a definition once, before broadcasting matching metrics.
func (c *simulationControl) publishTopology(document *model.TopologyDocument) error {
	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	c.topology.Store(&publishedTopology{document: encoded})
	return nil
}

// currentTopology copies the latest published definition without reading the model.
func (c *simulationControl) currentTopology() json.RawMessage {
	current := c.topology.Load()
	if current == nil {
		return nil
	}
	return append(json.RawMessage(nil), current.document...)
}

// clearTopology discards the graph together with the stopped simulation.
func (c *simulationControl) clearTopology() {
	c.topology.Store(nil)
}
