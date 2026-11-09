package main

import (
	"errors"
	"fmt"
	"load-balancer/scenario"
	"math"
)

// scenarioDefinition keeps the old browser defaults until its v2 migration.
// Only source DTOs cross this boundary; runtime settings stay native v2.
func (c configuration) scenarioDefinition() scenario.Scenario {
	definition := c.scenarioDefinitionV2()
	initial := definition.States["0"]
	lb := initial.Resources.LoadBalancer
	backends := make([]scenario.BackendConfig, 0, len(initial.Resources.Backends))
	for _, backend := range initial.Resources.Backends {
		backends = append(backends, scenario.BackendConfig{
			ID: backend.ID, Name: backend.Name, Address: backend.Address, Port: backend.Port,
			Network: backend.Network, Host: backend.Host,
		})
	}
	return scenario.Scenario{
		Version:   scenario.Version,
		Execution: definition.Fixed.Execution,
		TCP:       definition.Fixed.TCP,
		Network:   definition.Fixed.Network,
		Resources: scenario.ResourcesConfig{
			LoadBalancer: scenario.LoadBalancerConfig{
				ID:        definition.Fixed.LoadBalancer.ID,
				Algorithm: lb.Algorithm, Host: lb.Host, Ingress: lb.Ingress,
				ForwardingBitsPerSecond: lb.ForwardingBitsPerSecond,
				HalfOpenIdleTimeout:     lb.MappingTimeouts.HalfOpenIdleTimeout,
				EstablishedIdleTimeout:  lb.MappingTimeouts.EstablishedIdleTimeout,
				ClosingIdleTimeout:      lb.MappingTimeouts.ClosingIdleTimeout,
				ResetIdleTimeout:        lb.MappingTimeouts.ResetIdleTimeout,
			},
			Backends: backends,
		},
		Workload: scenario.WorkloadConfig{
			Defaults:     definition.Fixed.Workload.Defaults,
			ClientGroups: initial.Workload.ClientGroups,
		},
	}
}

// preparedScenario keeps requested settings beside their checked runtime values.
// For example, definition retains Bytes=-1 while resolved contains the byte count.
type preparedScenario struct {
	definition scenario.Scenario
	resolved   scenario.Normalized
}

// prepareScenario builds and normalizes one scenario without changing defaults.
func (c configuration) prepareScenario() (preparedScenario, error) {
	if c.clients < 0 || c.clients > scenario.MaxClients || c.servers < 0 {
		return preparedScenario{}, errors.New("invalid client or server count")
	}
	if c.servers > scenario.MaxBackends {
		return preparedScenario{}, fmt.Errorf("server count must not exceed %d", scenario.MaxBackends)
	}
	if c.speed < 0 || math.IsNaN(c.speed) || math.IsInf(c.speed, 0) {
		return preparedScenario{}, errors.New("speed must be finite and nonnegative")
	}

	definition := c.scenarioDefinition()
	runtimeDefinition := definition
	// Headless ignores playback speed after validating the requested value.
	if c.headless {
		runtimeDefinition.Execution.Speed = 0
	}

	resolved, err := scenario.Normalize(runtimeDefinition)
	if err != nil {
		return preparedScenario{}, err
	}
	return preparedScenario{definition: definition, resolved: resolved}, nil
}
