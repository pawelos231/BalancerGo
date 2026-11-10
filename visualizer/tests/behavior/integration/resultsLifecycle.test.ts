import assert from 'node:assert/strict';
import test from 'node:test';
import { CommandType } from '../../../src/commands.ts';
import { StopCommands } from '../../../src/hooks/stopCommands.ts';
import { ResultsCollection } from '../../../src/results/collection.ts';
import { ResultsSession } from '../../../src/results/session.ts';
import type { SimulationEnded } from '../../../src/telemetry/types.ts';
import type { StopCommandResult } from '../../../src/types.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

const intent = (runId = 'A', commandId = `stop-${runId}`) => ({ runId, commandId });

function end(runId = 'A', reason: SimulationEnded['reason'] = 'finished', commandId?: string): SimulationEnded {
  const frame = telemetryFrame('1', runId, 2, '2');
  frame.simulation.status = reason === 'stopped' ? 'running' : reason;
  return {
    type: 'simulationEnded', runId, reason, commandId,
    finalSnapshot: frame, topologyDefinition: topologyDefinition('1', runId),
  };
}

function acknowledgement(runId = 'A', status: StopCommandResult['status'] = 'applied', commandId = `stop-${runId}`): StopCommandResult {
  return {
    type: 'commandResult', commandType: CommandType.StopSimulation,
    runId, commandId, status,
  };
}

async function settle() {
  for (let index = 0; index < 8; index++) await Promise.resolve();
}

function fixture(options: ConstructorParameters<typeof ResultsSession>[1] = {}) {
  const collection = new ResultsCollection();
  let nextId = 0;
  const session = new ResultsSession(collection, { newId: () => `result-${++nextId}`, ...options });
  const ended = (event: SimulationEnded) => {
    // This is the hook's order: archive sources synchronously before policy/UI.
    if (session.acceptsRun(event.runId)) {
      if (event.finalSnapshot) collection.snapshot(event.finalSnapshot, 102, true);
      if (event.topologyDefinition) collection.definition(event.topologyDefinition, 102, true);
    }
    session.receiveEnd(event, 102);
  };
  return { collection, session, ended };
}

test('only a successful end event creates a candidate; ordinary finished snapshots and failures do not', async () => {
  const { collection, session, ended } = fixture();
  let notices = 0;
  const unsubscribe = session.subscribe(() => { notices++; });
  const initial = session.state;
  const regularFinished = end().finalSnapshot!;
  collection.snapshot(regularFinished, 100);
  assert.equal(session.state, initial);
  assert.equal(notices, 0); // Receiving samples never wakes the Results view.
  ended(end('failed-run', 'failed'));
  const executionError = end('error-run');
  executionError.finalSnapshot!.simulation.error = 'event limit reached';
  ended(executionError);
  await settle();
  assert.equal(session.state.candidates.length, 0);

  ended(end());
  await settle();
  assert.equal(session.state.candidates[0].runId, 'A');
  assert.equal(session.state.candidates[0].completion.reason, 'finished');
  assert.equal(session.state.candidates[0].collection.samples.length, 1);
  assert.ok(Object.isFrozen(session.state.candidates));
  assert.ok(Object.isFrozen(session.state.candidates[0]));
  assert.equal(notices, 2); // Preparing, then the immutable candidate.
  unsubscribe();
  session.release(session.state.candidates[0].id);
  assert.equal(notices, 2);
});

test('clear intent precedes send and suppresses an already queued natural end until confirmation', async () => {
  const { collection, session, ended } = fixture();
  const commands = new StopCommands();
  collection.snapshot(telemetryFrame('1', 'A'), 100);
  assert.equal(commands.send('A', 'stop-A', (pending) => {
    session.requestStop(pending, 'clear');
    // A synchronous delivery during socket.send sees the registered intent.
    ended(end());
  }), true);
  await settle();
  assert.equal(session.state.candidates.length, 0);
  assert.ok(collection.get('A'));
  session.receiveStopResult(acknowledgement(), 103);
  await settle();
  assert.equal(collection.get('A'), undefined);
  assert.equal(session.state.candidates.length, 0);
  ended(end()); // Replayed final data cannot resurrect a cleared run.
  assert.equal(collection.get('A'), undefined);
  assert.equal(session.state.candidates.length, 0);
});

test('rejection and a send failure both roll back the stop policy and reconsider a queued natural end', async () => {
  for (const rollback of ['rejection', 'send failure'] as const) {
    const { session, ended } = fixture();
    const pending = intent();
    session.requestStop(pending, 'clear');
    ended(end());
    assert.equal(session.state.candidates.length, 0);
    if (rollback === 'rejection') session.receiveStopResult(acknowledgement('A', 'rejected'));
    else session.cancelStop(pending);
    await settle();
    assert.equal(session.state.candidates.length, 1, rollback);
    assert.equal(session.state.candidates[0].completion.reason, 'finished');
  }
});

test('save racing a natural end and duplicate end/ack deliveries creates one result for either proof order', async () => {
  for (const firstProof of ['end', 'ack'] as const) {
    const { session, ended } = fixture();
    session.requestStop(intent(), 'save');
    ended(end()); // Natural completion waits for this client's pending Stop.
    assert.equal(session.state.preparing.length, 0);
    const stopped = end('A', 'stopped', 'stop-A');
    if (firstProof === 'end') ended(stopped);
    else session.receiveStopResult(acknowledgement());
    ended(stopped);
    ended(end());
    session.receiveStopResult(acknowledgement());
    await settle();
    assert.equal(session.state.candidates.length, 1, firstProof);
    assert.equal(session.state.candidates[0].completion.reason, 'finished');
  }

  const { session, ended } = fixture();
  session.requestStop(intent(), 'save');
  ended(end('A', 'stopped', 'someone-else'));
  await settle();
  assert.equal(session.state.candidates.length, 0); // Another client's Stop is not our proof.
  ended(end('A', 'stopped', 'stop-A'));
  await settle();
  assert.equal(session.state.candidates[0].completion.reason, 'stopped');
});

test('a late A stop cannot change B, and A/B/A end replay stays deduplicated', async () => {
  const { collection, session, ended } = fixture();
  session.requestStop(intent(), 'clear');
  collection.snapshot(telemetryFrame('1', 'B'), 101);
  session.requestStop(intent('B'), 'save');
  ended(end('A'));
  ended(end('B', 'stopped', 'stop-B'));
  session.receiveStopResult(acknowledgement());
  session.receiveStopResult(acknowledgement('B'));
  ended(end('A'));
  await settle();
  assert.deepEqual(session.state.candidates.map((candidate) => candidate.runId), ['B']);
  assert.equal(collection.get('A'), undefined);
  assert.deepEqual(collection.get('B')!.samples.map((sample) => sample.publicationSequence), ['1', '2']);

  ended(end('C'));
  ended(end('B'));
  ended(end('C'));
  await settle();
  assert.deepEqual(session.state.candidates.map((candidate) => candidate.runId), ['B', 'C']);
});

test('disconnect retains uncertain data and independent intent until matching proof arrives', async () => {
  const { collection, session, ended } = fixture();
  const commands = new StopCommands();
  commands.send('A', 'stop-A', (pending) => { session.requestStop(pending, 'save'); });
  collection.snapshot(telemetryFrame('1', 'A'), 100);
  commands.disconnect(); // The existing command tracker intentionally forgets it.
  session.disconnected();
  ended(end());
  session.receiveStopResult(acknowledgement('A', 'applied', 'unrelated'));
  await settle();
  assert.equal(session.state.candidates.length, 0);
  assert.match(session.state.warning!, /Connection lost/);
  assert.equal(collection.get('A')!.samples.length, 2);
  session.receiveStopResult(acknowledgement());
  await settle();
  assert.equal(session.state.candidates.length, 1);
  assert.equal(session.state.warning, null);
});

test('after reconnect a new manual Stop uses its own mode and proof while an older Stop stays uncertain', async () => {
  for (const mode of ['save', 'clear'] as const) {
    const { collection, session, ended } = fixture();
    const commands = new StopCommands();
    commands.send('A', 'old-stop', (pending) => {
      assert.equal(session.requestStop(pending, mode === 'save' ? 'clear' : 'save'), true);
    });
    collection.snapshot(telemetryFrame('1', 'A'), 100);
    commands.disconnect();
    session.disconnected();
    ended(end()); // The old uncertain command still defers this natural end.

    assert.equal(commands.send('A', 'manual-stop', (pending) => {
      assert.equal(session.requestStop(pending, mode), true);
    }), true);
    session.receiveStopResult(acknowledgement('A', 'applied', 'old-stop'));
    await settle();
    assert.equal(session.state.candidates.length, 0);
    assert.ok(collection.get('A')); // Old proof cannot choose the new mode or erase its measurements.
    assert.equal(commands.pending?.commandId, 'manual-stop');

    const result = acknowledgement('A', 'applied', 'manual-stop');
    session.receiveStopResult(result);
    assert.equal(commands.finish(result), true);
    ended(end('A', 'stopped', 'manual-stop'));
    session.receiveStopResult(result);
    await settle();
    assert.equal(session.state.candidates.length, mode === 'save' ? 1 : 0, mode);
    assert.equal(session.state.warning, null);
    if (mode === 'save') assert.equal(session.state.candidates[0].collection.samples.length, 2);
    else assert.equal(collection.get('A'), undefined);
  }
});

test('a rejected or failed replacement restores the old uncertain clear without auto-saving a queued end', async () => {
  for (const rollback of ['rejection', 'send failure'] as const) {
    for (const priorProof of ['before rollback', 'after rollback'] as const) {
      const { collection, session, ended } = fixture();
      const commands = new StopCommands();
      commands.send('A', 'old-clear', (pending) => {
        assert.equal(session.requestStop(pending, 'clear'), true);
      });
      commands.disconnect();
      session.disconnected();
      ended(end());
      const sendReplacement = () => commands.send('A', 'new-save', (pending) => {
        assert.equal(session.requestStop(pending, 'save'), true);
        if (priorProof === 'before rollback') {
          ended(end('A', 'stopped', 'old-clear')); // Cache the old proof, without applying the new save mode.
        }
        if (rollback === 'send failure') throw new Error('socket send failed');
      });
      if (rollback === 'send failure') {
        assert.throws(sendReplacement, /socket send failed/);
        session.cancelStop(intent('A', 'new-save'));
      } else {
        assert.equal(sendReplacement(), true);
        const rejected = acknowledgement('A', 'rejected', 'new-save');
        session.receiveStopResult(rejected);
        commands.finish(rejected);
      }
      await settle();
      assert.equal(session.state.candidates.length, 0, `${rollback}, ${priorProof}`);
      if (priorProof === 'after rollback') {
        assert.ok(collection.get('A'));
        assert.match(session.state.warning!, /Connection lost/);
        session.receiveStopResult(acknowledgement('A', 'applied', 'old-clear'));
      }
      assert.equal(collection.get('A'), undefined);
      assert.equal(session.state.warning, null);
    }
  }
});

test('repeated manual replacements remain bounded and roll back to the oldest unresolved policy', async () => {
  const { collection, session, ended } = fixture();
  session.requestStop(intent('A', 'first-clear'), 'clear');
  session.disconnected();
  assert.equal(session.requestStop(intent('A', 'second-save'), 'save'), true);
  session.disconnected();
  assert.equal(session.requestStop(intent('A', 'third-save'), 'save'), true);
  ended(end());
  session.receiveStopResult(acknowledgement('A', 'rejected', 'second-save'));
  session.receiveStopResult(acknowledgement('A', 'rejected', 'third-save'));
  await settle();
  assert.equal(session.state.candidates.length, 0);
  assert.ok(collection.get('A'));
  assert.match(session.state.warning!, /Connection lost/);
  session.receiveStopResult(acknowledgement('A', 'applied', 'first-clear'));
  assert.equal(collection.get('A'), undefined);
  assert.equal(session.state.warning, null);

  // Reconnect clicks cannot grow retained policy IDs without a fixed limit.
  for (let index = 0; index < 256; index++) {
    session.disconnected();
    assert.equal(session.requestStop(intent('B', `manual-${index}`), 'clear'), true);
  }
  session.disconnected();
  assert.equal(session.requestStop(intent('B', 'over-limit'), 'save'), false);
  session.receiveStopResult(acknowledgement('B', 'applied', 'manual-255'));
  assert.equal(collection.get('B'), undefined);
  assert.equal(session.requestStop(intent('C'), 'clear'), true); // Settling the chain returns its capacity.
});

test('a fast end waits once for submitted metadata, and later clear does not erase an already eligible end', async () => {
  let supplyMetadata!: () => void;
  let preparations = 0;
  let aborted = false;
  const { collection, session, ended } = fixture({
    prepareMetadata: (runId, signal) => {
      preparations++;
      signal.addEventListener('abort', () => { aborted = true; });
      return new Promise<void>((resolve) => {
        supplyMetadata = () => {
          collection.submitted(runId, scenarioFixture(), 103);
          resolve();
        };
      });
    },
  });
  ended(end());
  ended(end());
  await settle();
  assert.equal(preparations, 1);
  assert.deepEqual(session.state.preparing, ['A']);
  session.requestStop(intent(), 'clear');
  session.receiveStopResult(acknowledgement());
  supplyMetadata();
  await settle();
  assert.equal(aborted, false);
  const candidate = session.state.candidates[0];
  assert.equal(candidate.context.scenarioProvenance, 'submitted');
  assert.equal(candidate.coverage.metadataAfterFirstSample, true);
  assert.equal(candidate.completion.reason, 'finished');
  session.release(candidate.id);
  assert.equal(collection.get('A'), undefined);
  assert.equal(session.acceptsRun('A'), false);
  assert.equal(session.acceptsRun('B'), true);
  // Late HTTP metadata, topology and cached snapshots share the adapter gate.
  if (session.acceptsRun('A')) {
    collection.submitted('A', scenarioFixture(), 104);
    collection.definition(topologyDefinition('1', 'A'), 104);
    collection.snapshot(telemetryFrame('1', 'A'), 104);
  }
  assert.equal(collection.get('A'), undefined);
  ended(end());
  await settle();
  assert.equal(session.state.candidates.length, 0);
  assert.equal(preparations, 1);
});

test('null-sample saves remain explicit; capacity keeps fitting data and disposal aborts preparation', async () => {
  const { session } = fixture();
  session.requestStop(intent(), 'save');
  session.receiveStopResult(acknowledgement());
  await settle();
  assert.equal(session.state.candidates[0].summary.basis, 'unavailable');
  assert.equal(session.state.candidates[0].coverage.terminalSampleMissing, true);

  const limited = new ResultsCollection({ maxRuns: 1 });
  let preparations = 0;
  const capped = new ResultsSession(limited, { newId: () => 'kept-result' });
  limited.snapshot(telemetryFrame('1', 'kept'), 100);
  capped.requestStop(intent('missing'), 'save');
  capped.receiveStopResult(acknowledgement('missing'));
  await settle();
  assert.ok(limited.get('kept'));
  assert.equal(limited.usage.runCount, 1);
  assert.equal(capped.state.candidates.length, 0);
  assert.match(capped.state.warning!, /memory is full/);

  let signal: AbortSignal | undefined;
  const disposing = fixture({
    prepareMetadata: (_runId, receivedSignal) => {
      preparations++;
      signal = receivedSignal;
      return new Promise(() => {});
    },
  });
  disposing.ended(end());
  await settle();
  disposing.session.dispose();
  assert.equal(preparations, 1);
  assert.equal(signal?.aborted, true);
  assert.equal(disposing.session.acceptsRun('B'), false);
  disposing.ended(end('B'));
  assert.equal(disposing.session.state.candidates.length, 0);
});

test('a confirmed save can use the terminal reserve without a provisional allocation failure warning', async () => {
  const collection = new ResultsCollection({ byteBudget: 4096, terminalReserveBytes: 2048 });
  const session = new ResultsSession(collection, { newId: () => 'reserved-result' });
  assert.equal(session.requestStop(intent(), 'save'), true);
  assert.equal(collection.usage.runCount, 0); // Ordinary allocation cannot spend the terminal reserve.
  assert.equal(session.state.warning, null);
  session.receiveStopResult(acknowledgement());
  await settle();
  assert.equal(session.state.candidates.length, 1);
  assert.equal(session.state.candidates[0].id, 'reserved-result');
  assert.equal(session.state.warning, null);
  assert.ok(collection.usage.serializedBytes <= collection.usage.byteBudget);
});

test('new running runs release older ineligible history while keeping a current failed run and uncertain Stops', async () => {
  const { collection, session, ended } = fixture();
  session.observeRun('failed-current');
  collection.snapshot(telemetryFrame('1', 'failed-current'), 100);
  ended(end('failed-current', 'failed'));
  assert.equal(collection.get('failed-current')!.samples.length, 2);
  session.requestStop(intent('failed-current'), 'save');
  session.receiveStopResult(acknowledgement('failed-current'));
  await settle();
  assert.equal(session.state.candidates[0].collection.samples.length, 2);

  session.observeRun('uncertain');
  session.requestStop(intent('uncertain'), 'save');
  ended(end('uncertain', 'failed'));
  session.disconnected();
  session.observeRun('B');
  assert.ok(collection.get('uncertain'));
  ended(end('old-remote', 'stopped'));
  assert.equal(collection.get('old-remote'), undefined); // Its late end cannot consume a slot forever.
  session.receiveStopResult(acknowledgement('uncertain', 'rejected'));
  assert.equal(collection.get('uncertain'), undefined);
  assert.ok(collection.get('failed-current')); // An eligible candidate remains retained.

  for (let index = 0; index < 12; index++) {
    const runId = `failed-${index}`;
    session.observeRun(runId);
    ended(end(runId, 'failed'));
  }
  assert.equal(collection.usage.runCount, 2); // One candidate and the latest failed run.
  assert.equal(collection.usage.runLimitReached, false);
});
