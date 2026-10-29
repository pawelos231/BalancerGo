package scenario

import (
	"encoding/json"
	"strings"
	"testing"

	"load-balancer/model"
)

func TestTCPReceiveBufferDefaultsAndLimits(t *testing.T) {
	// Existing documents omit TCP settings and retain the default capacity.
	input := exampleScenario()
	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TCP.ReceiveBufferBytes != model.DefaultReceiveBufferBytes {
		t.Fatalf("default receive buffer = %d", resolved.TCP.ReceiveBufferBytes)
	}
	if input.TCP.ReceiveBufferBytes != nil {
		t.Fatal("normalization changed the requested settings")
	}

	// A tiny positive buffer is valid; zero must not become an unlimited buffer.
	for _, capacity := range []int{1, 64, 65535, 0, -1, 65536} {
		input.TCP.ReceiveBufferBytes = pointer(capacity)
		resolved, err := Normalize(input)
		valid := capacity >= 1 && capacity <= 65535
		if !valid {
			if err == nil || !strings.Contains(err.Error(), "tcp.receiveBufferBytes") {
				t.Fatalf("capacity %d: expected receive-buffer error, got %v", capacity, err)
			}
			continue
		}

		if err != nil || resolved.TCP.ReceiveBufferBytes != capacity {
			t.Fatalf("capacity %d: got %+v, error %v", capacity, resolved.TCP, err)
		}
	}
}

func TestTCPReceiveBufferSurvivesDocumentRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.TCP.ReceiveBufferBytes = pointer(64)

	document, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if restored.TCP.ReceiveBufferBytes == nil || *restored.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the requested receive buffer did not survive import/export")
	}

	// The resolved view exposes the effective value rather than an optional patch.
	resolved, err := Normalize(restored)
	if err != nil {
		t.Fatal(err)
	}
	document, err = EncodeNormalizedDocument(resolved)
	if err != nil {
		t.Fatal(err)
	}
	var object struct {
		TCP NormalizedTCPConfig `json:"tcp"`
	}
	if err := json.Unmarshal(document, &object); err != nil {
		t.Fatal(err)
	}
	if object.TCP.ReceiveBufferBytes != 64 {
		t.Fatal("the resolved document omitted the effective receive buffer")
	}
}
