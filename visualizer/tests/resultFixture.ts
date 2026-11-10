import { assembleRunResult } from '../src/results/assembly.ts';
import { ResultsCollection } from '../src/results/collection.ts';
import type { RunResult } from '../src/results/types.ts';
import type { CurrentSimulation } from '../src/scenarios/types.ts';
import { readTelemetryJSON } from '../src/telemetry/exact.ts';
import type { SimulationSnapshot } from '../src/telemetry/types.ts';
import { experimentFixture, scenarioFixture } from './scenarioFixture.ts';
import { telemetryFrame, topologyDefinition } from './telemetryFixture.ts';

// A full archive fixture includes exact wire integers, nullable coverage,
// scenario provenance, transfer/connection details and every resource family.
export function resultFixture(id = 'local-result', runId = 'run-1'): RunResult {
  const collection = new ResultsCollection();
  const scenario = scenarioFixture();
  scenario.fixed.tcp = {
    mssBytes: 1460, receiveBufferBytes: null, sackEnabled: true, ECNenabled: false,
    delayedAck: { enabled: true, segmentsPerACK: 2, maxDelayNanoseconds: 1_000 },
    rto: { mode: 'adaptive', exponentialBackoff: true, initialTimeoutNanoseconds: 100, minimumTimeoutNanoseconds: 10, maximumTimeoutNanoseconds: 1_000 },
    close: { mslNanoseconds: 100, finWaitTimeoutNanoseconds: 200, maxFINRetries: 3 },
    congestionControl: { mode: 'cubic', fastRetransmit: true, cubicFastConvergence: true, initialWindowSegments: 2, maximumWindowSegments: null, initialSlowStartThresholdBytes: 65_535 },
  };
  const network = { profile: 'old-profile', toLoadBalancer: {}, fromLoadBalancer: { queueCapacity: null } };
  scenario.states['0'].resources.backends = [{
    id: 'srv', name: 'Backend', address: '10.0.0.1', port: 80, network,
    host: { cores: 2, packetProcessingTimeNanoseconds: 100, queueCapacity: null },
    physicalAvailable: true, administrativeState: 'active',
  }];
  scenario.states['0'].resources.loadBalancer.host = { cores: 2 };
  scenario.states['0'].workload.clientGroups = [{
    id: 'clients', count: 2, startAtNanoseconds: 0, clientGapNanoseconds: 100,
    network, stream: { bytes: 2048, packets: null },
    overrides: [{ index: 1, network, stream: {} }],
  }];
  const metadata = readTelemetryJSON(JSON.stringify({
    runId, status: 'running', scenario, experiment: experimentFixture(),
  }).replaceAll('"speed":0', '"speed":10000000000000000')) as CurrentSimulation;
  collection.metadata(metadata, 10);
  collection.definition(topologyDefinition('1', runId), 20);
  const frame = telemetryFrame('1', runId, 1, '3');
  frame.simulation.payload = { receivedBytes: 42, acknowledgedBytes: 41 };
  frame.simulation.performance = {
    wallTimeSeconds: 1, eventsExecuted: 43, eventsPerWallSecond: 100,
    forwardedPackets: 20, forwardedPacketsPerWallSecond: 20,
  };
  const duration = { samples: 2, meanSeconds: 0.1, minSeconds: 0.05, maxSeconds: 0.15 };
  frame.simulation.transfers = {
    started: 3, notStarted: 0, succeeded: 1, failed: 1, inProgress: 1,
    failures: { backendRemoved: 1 }, synRetransmissions: 2, dataRetransmissions: 3,
    handshake: { ...duration }, payload: { ...duration }, completion: { ...duration },
  };
  frame.servers[0].clients[0].transfer = {
    status: 'succeeded', elapsedSeconds: 0.2, handshakeCompleted: true,
    handshakeDurationSeconds: 0.1, payloadDurationSeconds: 0.1,
    synRetransmissions: 1, dataRetransmissions: 0,
  };
  frame.servers[0].clients[0].connection = { state: 'timeWait', outcome: 'graceful', finRetransmissions: 1, elapsedNanoseconds: 2_000_000 };
  const exact = readTelemetryJSON(JSON.stringify(frame)
    .replace('"receivedBytes":42', '"receivedBytes":18446744073709551615')
    .replace('"eventsExecuted":43', '"eventsExecuted":9007199254740993')) as SimulationSnapshot;
  collection.snapshot(exact, 100, true);
  return assembleRunResult(collection.get(runId)!, { id, completion: { reason: 'finished', receivedAt: 200 } });
}
