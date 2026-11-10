import assert from 'node:assert/strict';
import test from 'node:test';
import { CommandType } from '../../../src/commands.ts';
import { ResultsCollection } from '../../../src/results/collection.ts';
import { ResultsSession } from '../../../src/results/session.ts';
import { RunLifecycleReceiver } from '../../../src/telemetry/lifecycle.ts';
import { parseSimulationEnded } from '../../../src/telemetry/lifecycleParse.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

// A fast failed run can deliver only its terminal message. It must remain
// available for an explicit save until a newer run becomes the displayed run.
test('terminal-only failures retain the current history and release the previous one', async () => {
  const collection = new ResultsCollection();
  const session = new ResultsSession(collection, { newId: () => 'saved-failure' });
  const lifecycle = new RunLifecycleReceiver(() => {});

  function failed(runId: string) {
    const frame = telemetryFrame('1', runId, 1, '1');
    frame.simulation.status = 'failed';
    frame.simulation.error = 'Execution stopped unexpectedly.';
    const parsed = parseSimulationEnded({
      type: 'simulationEnded', runId, reason: 'failed',
      finalSnapshot: frame, topologyDefinition: topologyDefinition('1', runId),
    });
    const sample = parsed.sample!;
    const capture = () => {
      collection.definition(sample.definition, 100, true);
      collection.snapshot(sample.snapshot, 100, true);
    };

    capture();
    const receipt = lifecycle.receiveEnd(parsed);
    if (receipt.display) {
      session.observeRun(runId);
      capture();
    }
    session.receiveEnd(parsed.event, 100);
  }

  failed('run-a');
  assert.ok(collection.get('run-a'));
  assert.equal(session.state.candidates.length, 0);

  failed('run-b');
  assert.equal(collection.get('run-a'), undefined);
  assert.ok(collection.get('run-b'));

  // Explicit save is allowed; the execution error remains in the saved source.
  const intent = { runId: 'run-b', commandId: 'save-b' };
  session.requestStop(intent, 'save');
  session.receiveStopResult({
    type: 'commandResult', commandType: CommandType.StopSimulation,
    ...intent, status: 'applied',
  }, 101);
  for (let index = 0; index < 6; index++) await Promise.resolve();

  assert.equal(session.state.candidates.length, 1);
  assert.equal(session.state.candidates[0].collection.samples[0].snapshot.simulation.error,
    'Execution stopped unexpectedly.');
  session.dispose();
});
