package contract

import "encoding/json"

// MarshalJSON writes the original metric tokens, e.g. 9007199254740993 bytes.
// The public Go float values are retained for existing display/report callers.
func (node TelemetryNode) MarshalJSON() ([]byte, error) {
	type plain TelemetryNode
	if node.ExactMetrics == nil {
		return json.Marshal(plain(node))
	}
	return json.Marshal(struct {
		plain
		Metrics map[string]json.Number `json:"metrics"`
	}{plain(node), node.ExactMetrics})
}

// MarshalJSON keeps constant limits exact in HTTP topology definitions too.
func (metric TopologyMetricDefinition) MarshalJSON() ([]byte, error) {
	type plain TopologyMetricDefinition
	if metric.ExactValue == nil {
		return json.Marshal(plain(metric))
	}
	return json.Marshal(struct {
		plain
		Value *json.Number `json:"value,omitempty"`
	}{plain(metric), metric.ExactValue})
}
