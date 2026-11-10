package presentation

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"load-balancer/host"
	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/model"
	"load-balancer/network"
)

// Existing small-number checks compare displayed values; exactness is checked below.
func numericMetric(t *testing.T, value json.Number) float64 {
	t.Helper()
	if value == "" {
		return 0
	}
	number, err := value.Float64()
	if err != nil {
		t.Fatal(err)
	}
	return number
}

// Counters above JavaScript's safe range keep every digit through real JSON output.
// The numeric Go projection remains available for existing live-view callers.
func TestMetricJSONPreservesNativeIntegers(t *testing.T) {
	const large = int64(9_007_199_254_740_993)
	info := simulation.Info{
		Forwarding: network.ForwardingStats{
			ForwardingBitsPerSecond: large, ForwardedBytes: math.MaxUint64,
		},
	}
	metrics := loadBalancerMetrics(info, simulation.IngressObservation{}, 1, 0, contract.TransferSummary{})
	metrics = appendHostMetrics(metrics, host.Stats{
		Enabled: true, Cores: 1, ProcessedPackets: math.MaxUint64,
	})
	node := contract.TelemetryNode{
		ID: "lb", Metrics: metricValues(metrics), ExactMetrics: exactMetricValues(metrics),
	}
	document := struct {
		Node        contract.TelemetryNode              `json:"node"`
		Definitions []contract.TopologyMetricDefinition `json:"definitions"`
	}{node, metricDefinitions(metrics)}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Node struct {
			Metrics map[string]json.Number `json:"metrics"`
		} `json:"node"`
		Definitions []struct {
			Key   string      `json:"key"`
			Value json.Number `json:"value"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"forwardedBytes", "hostProcessedPackets"} {
		if got := decoded.Node.Metrics[key]; got != "18446744073709551615" {
			t.Fatalf("%s JSON = %s, want full uint64", key, got)
		}
	}
	for _, metric := range decoded.Definitions {
		if metric.Key == "forwardingBitsPerSecond" && metric.Value != "9007199254740993" {
			t.Fatalf("static HTTP limit was rounded: %s", metric.Value)
		}
	}
}

// Byte/sequence counters stay integral while an explicitly fractional delay stays fractional.
func TestClientAndBackendMetricTokens(t *testing.T) {
	client := simulation.ClientObservation{
		State: model.ClientState{AcknowledgedPayloadBytes: math.MaxUint64},
	}
	client.Config.Stream.PacketGap = 1500 * time.Microsecond
	for _, test := range []struct {
		metrics []nodeMetric
		key     string
		want    json.Number
	}{
		{clientMetrics(client), "acknowledgedPayloadBytes", "18446744073709551615"},
		{clientMetrics(client), "packetGapMs", "1.5"},
		{backendMetrics(model.ServerState{ReceivedPayloadBytes: math.MaxUint64}), "receivedPayloadBytes", "18446744073709551615"},
	} {
		found := false
		for _, metric := range test.metrics {
			if metric.key == test.key {
				found = true
				if metric.value != test.want {
					t.Fatalf("%s = %s, want %s", test.key, metric.value, test.want)
				}
			}
		}
		if !found {
			t.Fatalf("missing metric %s", test.key)
		}
	}
}
