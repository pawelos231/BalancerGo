import assert from 'node:assert/strict';
import test from 'node:test';
import { TelemetryReceiver } from '../../../src/telemetry/receiver.ts';
import { parseSnapshot } from '../../../src/telemetry/parse.ts';
import { PublicationTracker } from '../../../src/telemetry/publications.ts';
import type { TopologyDefinition } from '../../../src/telemetry/types.ts';
import { idleFrame, telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail; });
  return { promise, resolve, reject };
}

function receiverFixture() {
  const requests: ReturnType<typeof deferred<TopologyDefinition>>[] = [];
  const receiver = new TelemetryReceiver(() => {
    const request = deferred<TopologyDefinition>();
    requests.push(request);
    return request.promise;
  }, () => {});
  return { receiver, requests };
}

async function settle() {
  for (let index = 0; index < 6; index++) await Promise.resolve();
}

test('one in-flight request coalesces frames and hydrates only the latest measurements', async () => {
  const { receiver, requests } = receiverFixture();
  receiver.receive(telemetryFrame('1', 'run-1', 1, '1'));
  receiver.receive(telemetryFrame('1', 'run-1', 2, '2'));
  receiver.receive(telemetryFrame('1', 'run-1', 30, '3'));
  assert.equal(requests.length, 1);
  assert.equal(receiver.state.data?.simulation.virtualTimeSeconds, 3);
  assert.equal(receiver.state.loading, true);
  requests[0].resolve(topologyDefinition());
  await settle();
  assert.equal(receiver.state.loading, false);
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 3000);
  assert.equal(requests.length, 1);
});

test('the current TCP phase survives topology hydration', async () => {
  const { receiver, requests } = receiverFixture();
  const frame = telemetryFrame();
  const client = frame.topology.nodes.find((node) => node.id === 'client-a');
  assert.ok(client);
  client.tcpPhase = 'fastRecovery';

  // HTTP supplies stable identity; the live WebSocket frame supplies this phase.
  receiver.receive(frame);
  requests[0].resolve(topologyDefinition());
  await settle();
  const displayed = receiver.state.data?.topology?.nodes.find((node) => node.id === 'client-a');
  assert.equal(displayed?.tcpPhase, 'fastRecovery');
});

test('latest experiment context survives pending and older topology hydration, then clears on idle', async () => {
  const { receiver, requests } = receiverFixture();
  const first = telemetryFrame();
  receiver.receive(first);
  assert.equal(receiver.state.data?.simulation.experiment, first.simulation.experiment);
  const latest = telemetryFrame('2', 'run-1', 1, '2');
  latest.simulation.experiment!.execution.traceEnabled = true;
  receiver.receive(latest);
  requests[0].resolve(topologyDefinition('1'));
  await settle();
  assert.equal(receiver.state.data?.simulation.experiment, latest.simulation.experiment);
  requests[1].resolve(topologyDefinition('2'));
  await settle();
  assert.equal(receiver.state.data?.simulation.experiment, latest.simulation.experiment);
  receiver.receive(idleFrame());
  assert.equal(receiver.state.data?.simulation.experiment, undefined);
});

test('a behind response keeps a matched view while one follow-up catches the newest revision', async () => {
  const { receiver, requests } = receiverFixture();
  receiver.receive(telemetryFrame('1', 'run-1', 1, '1'));
  receiver.receive(telemetryFrame('2', 'run-1', 2, '2'));
  receiver.receive(telemetryFrame('3', 'run-1', 3, '3'));
  requests[0].resolve(topologyDefinition('1'));
  await settle();
  assert.equal(requests.length, 2);
  assert.equal(receiver.state.loading, true);
  assert.equal(receiver.state.data?.simulation.virtualTimeSeconds, 0.3);
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 100);
  requests[1].resolve(topologyDefinition('3'));
  await settle();
  assert.equal(receiver.state.loading, false);
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 300);
});

test('an ahead definition waits for its frame instead of spinning HTTP requests', async () => {
  const { receiver, requests } = receiverFixture();
  receiver.receive(telemetryFrame('1'));
  requests[0].resolve(topologyDefinition('2'));
  await settle();
  assert.equal(requests.length, 1);
  assert.equal(receiver.state.data?.topology, undefined);
  receiver.receive(telemetryFrame('2', 'run-1', 9, '2'));
  assert.equal(requests.length, 1);
  assert.equal(receiver.state.loading, false);
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 900);
});

test('Stop and another run discard stale HTTP responses without waiting for definitions', async () => {
  const { receiver, requests } = receiverFixture();
  receiver.receive(telemetryFrame());
  receiver.receive(idleFrame());
  assert.deepEqual(receiver.state.data?.topology, { nodes: [], links: [] });
  assert.equal(receiver.state.data?.simulation.status, 'idle');
  requests[0].resolve(topologyDefinition());
  await settle();
  assert.deepEqual(receiver.state.data?.topology, { nodes: [], links: [] });
  receiver.receive(telemetryFrame('1', 'run-2'));
  assert.equal(receiver.state.data?.simulation.runId, 'run-2');
  assert.equal(receiver.state.data?.topology, undefined);
  requests[1].resolve(topologyDefinition('1', 'run-1'));
  await settle();
  assert.equal(receiver.state.data?.topology, undefined);
});

test('reconnect forces a fresh definition even when the backend reuses the same run ID', async () => {
  const { receiver, requests } = receiverFixture();
  receiver.receive(telemetryFrame());
  receiver.reset();
  receiver.receive(telemetryFrame());
  assert.equal(requests.length, 2);
  requests[0].resolve(topologyDefinition());
  await settle();
  assert.equal(receiver.state.data?.topology, undefined);
  const fresh = topologyDefinition();
  fresh.servers[0].name = 'Restarted backend';
  requests[1].resolve(fresh);
  await settle();
  assert.equal(receiver.state.data?.servers[0].server.Name, 'Restarted backend');
});

test('failed definitions leave raw lifecycle usable and Retry recovers the last frame', async () => {
  const { receiver, requests } = receiverFixture();
  const finished = telemetryFrame();
  finished.simulation.status = 'finished';
  receiver.receive(finished);
  requests[0].reject(new Error('Network unavailable.'));
  await settle();
  assert.equal(receiver.state.data?.simulation.status, 'finished');
  assert.equal(receiver.state.loading, false);
  assert.equal(receiver.state.error, 'Network unavailable.');
  receiver.receive(finished);
  assert.equal(requests.length, 1);
  receiver.retry();
  assert.equal(requests.length, 2);
  requests[1].resolve(topologyDefinition());
  await settle();
  assert.equal(receiver.state.error, null);
  assert.equal(receiver.state.data?.topology?.nodes[2].traffic?.sampleWindowSeconds, 0.1);
  assert.equal(receiver.state.data?.simulation.status, 'finished');
});

test('validated delivery tracking sees gaps before receiver coalescing and prevents stale UI regression', async () => {
  const { receiver, requests } = receiverFixture();
  const publications = new PublicationTracker();
  function receive(value: unknown) {
    const snapshot = parseSnapshot(value);
    const observation = publications.observe(snapshot.simulation);
    if (observation.acceptSnapshot) receiver.receive(snapshot);
    return observation;
  }

  receive(telemetryFrame('1', 'run-1', 2, '2'));
  const gap = receive(telemetryFrame('1', 'run-1', 5, '5'));
  assert.equal(gap.kind, 'gap');
  assert.equal(gap.missingSinceLast, '2');
  assert.equal(requests.length, 1);
  const stale = telemetryFrame('1', 'run-1', 3, '3');
  stale.simulation.status = 'finished';
  assert.equal(receive(stale).acceptSnapshot, false);
  assert.equal(receiver.state.data?.simulation.status, 'running');
  assert.equal(receiver.state.data?.simulation.publicationSequence, '5');
  assert.equal(receiver.state.data?.simulation.virtualTimeSeconds, 0.5);

  requests[0].resolve(topologyDefinition());
  await settle();
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 500);
  assert.equal(receive(telemetryFrame('1', 'run-1', 5, '5')).newPublication, false);
  assert.equal(receiver.state.data?.simulation.publicationSequence, '5');
});

test('a cached finished publication rehydrates after reconnect without becoming a new measurement', async () => {
  const { receiver, requests } = receiverFixture();
  const publications = new PublicationTracker();
  const finished = telemetryFrame('1', 'run-1', 8, '8');
  finished.simulation.status = 'finished';
  function receive(value: unknown) {
    const snapshot = parseSnapshot(value);
    const observation = publications.observe(snapshot.simulation);
    if (observation.acceptSnapshot) receiver.receive(snapshot);
    return observation;
  }

  receive(finished);
  requests[0].resolve(topologyDefinition());
  await settle();
  publications.reconnect();
  receiver.reset();
  const cached = receive(finished);
  assert.equal(cached.kind, 'duplicate');
  assert.equal(cached.newPublication, false);
  assert.equal(cached.acceptSnapshot, true);
  assert.equal(cached.missingTotal, '0');
  assert.equal(requests.length, 2);
  assert.equal(receiver.state.loading, true);
  requests[1].resolve(topologyDefinition());
  await settle();
  assert.equal(receiver.state.loading, false);
  assert.equal(receiver.state.data?.simulation.status, 'finished');
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 800);
  assert.equal(receive(finished).acceptSnapshot, false);
});
