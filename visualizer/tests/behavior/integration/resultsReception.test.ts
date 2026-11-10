import assert from 'node:assert/strict';
import test from 'node:test';
import { ResultsCollection } from '../../../src/results/collection.ts';
import { TelemetryReceiver } from '../../../src/telemetry/receiver.ts';
import type { TopologyDefinition } from '../../../src/telemetry/types.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

async function settle() {
  for (let index = 0; index < 6; index++) await Promise.resolve();
}

// Three publications can reach the app before HTTP supplies a usable topology.
// Results keeps all three even when the live view needs only the newest one.
test('collection keeps publications before the live receiver coalesces them', async () => {
  const collection = new ResultsCollection();
  let supplyDefinition!: (definition: TopologyDefinition) => void;
  const receiver = new TelemetryReceiver(
    () => new Promise(resolve => { supplyDefinition = resolve; }),
    () => {},
  );

  for (let sequence = 1; sequence <= 3; sequence++) {
    const frame = telemetryFrame('1', 'run-1', sequence, String(sequence));
    frame.simulation.virtualTimeSeconds = 1;

    collection.snapshot(frame, 1000 + sequence);
    receiver.receive(frame);
  }

  const definition = topologyDefinition();
  collection.definition(definition);
  supplyDefinition(definition);
  await settle();

  assert.equal(collection.get('run-1')?.samples.length, 3);
  assert.equal(receiver.state.data?.topology?.links[0].stats.deliveredBytes, 300);
  receiver.dispose();
});
