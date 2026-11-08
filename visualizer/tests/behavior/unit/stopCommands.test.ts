import assert from 'node:assert/strict';
import test from 'node:test';
import { parseStopCommandResult, StopCommands } from '../../../src/hooks/stopCommands.ts';
import type { StopCommandResult } from '../../../src/types.ts';
import { CommandType } from '../../../src/commands.ts';

function result(overrides: Partial<StopCommandResult> = {}): StopCommandResult {
  return {
    type: 'commandResult', commandType: CommandType.StopSimulation,
    runId: 'run-a', commandId: 'stop-a', status: 'applied', ...overrides,
  };
}

test('Stop reserves both IDs before sending and accepts only the matching completion', () => {
  const stops = new StopCommands();
  assert.equal(stops.send('run-a', 'stop-a', (intent) => {
    assert.equal(stops.pending, intent);
    assert.deepEqual(intent, { runId: 'run-a', commandId: 'stop-a' });
  }), true);
  assert.equal(stops.send('run-a', 'stop-b', () => assert.fail('duplicate send')), false);
  assert.equal(stops.finish(result({ runId: 'run-b' })), false);
  assert.equal(stops.finish(result({ commandId: 'old-stop' })), false);
  assert.deepEqual(stops.pending, { runId: 'run-a', commandId: 'stop-a' });
  assert.equal(stops.finish(result()), true);
  assert.equal(stops.pending, null);
});

test('unrelated idle and finished snapshots never acknowledge Stop', () => {
  const stops = new StopCommands();
  stops.send('run-a', 'stop-a', () => {});
  assert.equal(stops.acceptsSnapshot({ status: 'idle' }), true);
  assert.equal(stops.acceptsSnapshot({ status: 'finished', runId: 'run-a' }), true);
  assert.deepEqual(stops.pending, { runId: 'run-a', commandId: 'stop-a' });
});

test('a matching rejection rolls back the intent without clearing the run', () => {
  const stops = new StopCommands();
  stops.send('run-a', 'stop-a', () => {});
  assert.equal(stops.finish(result({ status: 'rejected', error: 'Run changed.' })), true);
  assert.equal(stops.pending, null);
  assert.equal(stops.acceptsSnapshot({ status: 'running', runId: 'run-a' }), true);
  assert.equal(stops.send('run-a', 'stop-retry', () => {}), true);
});

test('a failed socket send rolls back its reservation and allows another explicit attempt', () => {
  const stops = new StopCommands();
  const failure = new Error('Socket closed.');
  assert.throws(() => stops.send('run-a', 'stop-a', () => {
    assert.deepEqual(stops.pending, { runId: 'run-a', commandId: 'stop-a' });
    throw failure;
  }), failure);
  assert.equal(stops.pending, null);
  assert.equal(stops.acceptsSnapshot({ status: 'finished', runId: 'run-a' }), true);
  assert.equal(stops.send('run-a', 'new-stop', () => {}), true);
});

test('applied before idle suppresses queued frames for A while accepting the next run B', () => {
  const stops = new StopCommands();
  stops.send('run-a', 'stop-a', () => {});
  assert.equal(stops.finish(result()), true);
  // A late current-scenario HTTP response must be discarded as well.
  assert.equal(stops.hasClearedRun('run-a'), true);
  assert.equal(stops.hasClearedRun('run-b'), false);
  assert.equal(stops.acceptsSnapshot({ status: 'finished', runId: 'run-a' }), false);
  assert.equal(stops.acceptsSnapshot({ status: 'idle' }), true);
  assert.equal(stops.acceptsSnapshot({ status: 'running', runId: 'run-b' }), true);
  assert.equal(stops.acceptsSnapshot({ status: 'running', runId: 'run-a' }), false);
});

test('late applied or rejected A replies cannot settle or clear the next Stop B', () => {
  const stops = new StopCommands();
  stops.send('run-a', 'stop-a', () => {});
  stops.finish(result());
  stops.send('run-b', 'stop-b', () => {});
  assert.equal(stops.finish(result()), false);
  assert.equal(stops.finish(result({ status: 'rejected', error: 'Old failure.' })), false);
  assert.deepEqual(stops.pending, { runId: 'run-b', commandId: 'stop-b' });
  assert.equal(stops.acceptsSnapshot({ status: 'running', runId: 'run-b' }), true);
  assert.equal(stops.finish(result({ runId: 'run-b', commandId: 'stop-b' })), true);
});

test('disconnect reports uncertainty, forgets the intent, and never replays it', () => {
  const stops = new StopCommands();
  let sent = 0;
  stops.send('run-a', 'stop-a', () => { sent++; });
  assert.equal(stops.disconnect(), true);
  assert.equal(stops.pending, null);
  assert.equal(stops.finish(result()), false);
  assert.equal(stops.disconnect(), false);
  assert.equal(stops.acceptsSnapshot({ status: 'running', runId: 'run-a' }), true);
  assert.equal(sent, 1);
});

test('Stop result parsing distinguishes completion frames from snapshots and legacy errors', () => {
  const applied = result();
  assert.equal(parseStopCommandResult(applied)?.result, applied);
  assert.ok(parseStopCommandResult(result({ runId: '', commandId: '', status: 'rejected' })));
  for (const message of [
    null,
    { type: 'simulationSnapshot', simulation: { status: 'idle' } },
    { type: 'commandError', commandType: CommandType.StopSimulation, error: 'Uncorrelated.' },
    { ...result(), commandType: CommandType.SpawnClient },
    { ...result(), commandId: undefined },
    { ...result(), status: 'accepted' },
  ]) assert.equal(parseStopCommandResult(message), null);
});
