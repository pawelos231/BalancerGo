import type { ScenarioPreset } from '../scenarioPresetTypes.ts';
import type { Scenario } from '../types.ts';
import { isolatedExperiment } from './base.ts';

// Only backend 2 changes; backend 1 keeps its 4 ms base RTT and 1 Gbit/s links.
const latencyCases = [
  { id: 'near', baseRTTMilliseconds: 4, packetTravelTimeNanoseconds: 1_000_000 },
  { id: 'far', baseRTTMilliseconds: 40, packetTravelTimeNanoseconds: 19_000_000 },
] as const;

const returnPathCases = [
  { id: 'fast-ack', label: '1 Gbit/s', bitsPerSecond: 1_000_000_000 },
  { id: 'slow-ack', label: '64 kbit/s', bitsPerSecond: 64_000 },
] as const;

const streamCases = [
  { id: 'short', label: '16 KiB', bytes: 16_384 },
  { id: 'long', label: '1 MiB', bytes: 1_048_576 },
] as const;

// Keep clients, LB limits and execution settings identical across the matrix.
function referenceScenario(
  defaults: Scenario,
  backendTravelTime: number,
  returnBitsPerSecond: number,
  streamBytes: number,
): Scenario {
  const scenario = isolatedExperiment(defaults);
  scenario.execution.durationNanoseconds = 60_000_000_000;
  scenario.workload.defaults.bytes = streamBytes;

  const backend = scenario.resources.backends[1];
  backend.network.fromLoadBalancer = {
    packetTravelTimeNanoseconds: backendTravelTime,
  };
  backend.network.toLoadBalancer = {
    packetTravelTimeNanoseconds: backendTravelTime,
    bandwidthBitsPerSecond: returnBitsPerSecond,
  };

  return scenario;
}

// Cross latency, return bandwidth and stream size to produce eight comparable cases.
function buildReferencePresets(): ScenarioPreset[] {
  const presets: ScenarioPreset[] = [];

  for (const latency of latencyCases) {
    for (const returnPath of returnPathCases) {
      for (const stream of streamCases) {
        presets.push({
          id: `reference-${latency.id}-${returnPath.id}-${stream.id}`,
          category: 'reference',
          title: `Reference: ${latency.baseRTTMilliseconds} ms / ${returnPath.id} / ${stream.label}`,
          description:
            `16 clients, two backends, ${stream.label} per client. ` +
            `Backend 2: ${latency.baseRTTMilliseconds} ms base travel RTT and ` +
            `${returnPath.label} backend-to-LB return link. ` +
            'Backend 1 stays at 4 ms and 1 Gbit/s; actual RTT also includes transmission and queues.',
          build(defaults) {
            return referenceScenario(
              defaults,
              latency.packetTravelTimeNanoseconds,
              returnPath.bitsPerSecond,
              stream.bytes,
            );
          },
        });
      }
    }
  }

  return presets;
}

export const referencePresets: readonly ScenarioPreset[] = buildReferencePresets();
