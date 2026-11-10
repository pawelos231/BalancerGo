import assert from 'node:assert/strict';
import test from 'node:test';
import { ResultsCollection } from '../../../src/results/collection.ts';
import { ResultsHistory } from '../../../src/results/history.ts';
import { ResultsSession } from '../../../src/results/session.ts';
import { assembleRunResult } from '../../../src/results/assembly.ts';
import type { ReadyResultEntry, ResultsRepository } from '../../../src/results/repository.ts';
import type { RunResult } from '../../../src/results/types.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

class MemoryRepository implements ResultsRepository {
  readonly values = new Map<string, RunResult>();
  failSave = false;
  writes = 0;

  private entry(result: RunResult): ReadyResultEntry {
    return {
      status: 'ready', id: result.id, runId: result.runId, resultVersion: 1,
      completionReason: result.completion.reason, receivedAt: result.completion.receivedAt,
      complete: result.coverage.complete, sampleCount: result.collection.samples.length,
      virtualTimeStartSeconds: 0, virtualTimeEndSeconds: 1,
      seed: result.context.seed, model: result.context.experiment?.model ?? null,
      algorithm: 'roundRobin',
    };
  }

  list() { return { entries: [...this.values.values()].map(result => this.entry(result)), warnings: [] }; }
  read(id: string) { return this.values.get(id) ?? null; }
  remove(id: string) { this.values.delete(id); }
  recover(id: string) { return this.read(id)!; }

  save(result: RunResult) {
    this.writes++;
    if (this.failSave) throw new Error('Storage quota reached.');
    this.values.set(result.id, result);
    return this.entry(result);
  }
}

function setup(repository = new MemoryRepository(), localId = 'local-result') {
  const collection = new ResultsCollection();
  const session = new ResultsSession(collection, { newId: () => localId });
  const history = new ResultsHistory(session, repository);

  function finish() {
    const frame = telemetryFrame();
    frame.simulation.status = 'finished';
    frame.simulation.payload = { receivedBytes: 1024, acknowledgedBytes: 1024 };
    const definition = topologyDefinition();
    collection.snapshot(frame, 100, true);
    collection.definition(definition, 100, true);
    session.receiveEnd({
      type: 'simulationEnded', runId: 'run-1', reason: 'finished',
      finalSnapshot: frame, topologyDefinition: definition,
    }, 100);
  }

  return { collection, session, repository, history, finish };
}

async function settle() {
  for (let index = 0; index < 8; index++) await Promise.resolve();
}

// A successful store releases both the candidate and its active collection.
test('successful completion persists once and releases retained RAM', async () => {
  const { collection, session, repository, history, finish } = setup();
  history.start();
  finish();
  await settle();

  assert.equal(repository.writes, 1);
  assert.equal(session.state.candidates.length, 0);
  assert.equal(collection.get('run-1'), undefined);
  assert.equal(history.getSnapshot().entries.length, 1);
  assert.equal(history.read('local-result')?.summary.payload?.acknowledgedBytes, 1024);
  history.stop();
  session.dispose();
});

// Full storage never consumes the only in-memory copy or creates a retry loop.
test('failed writes keep a candidate until an explicit retry succeeds', async () => {
  const { collection, session, repository, history, finish } = setup();
  repository.failSave = true;
  history.start();
  finish();
  await settle();

  assert.equal(repository.writes, 1);
  assert.ok(collection.get('run-1'));
  assert.equal(history.getSnapshot().candidates.length, 1);
  assert.match(history.getSnapshot().saveErrors['local-result'], /quota/);

  history.refresh();
  history.stop();
  history.start();
  assert.equal(repository.writes, 1);

  repository.failSave = false;
  history.retry('local-result');
  assert.equal(repository.writes, 2);
  assert.equal(history.getSnapshot().candidates.length, 0);
  assert.equal(history.getSnapshot().saveErrors['local-result'], undefined);
  history.stop();
  session.dispose();
});

// Mount probes and page subscriptions can stop and start without duplicate saves.
test('restarting history picks up an existing candidate exactly once', async () => {
  const { session, repository, history, finish } = setup();
  history.start();
  history.stop();
  finish();
  await settle();
  assert.equal(repository.writes, 0);

  history.start();
  history.stop();
  history.start();
  assert.equal(repository.writes, 1);
  history.stop();
  session.dispose();
});

// Reloading after completion receives the retained end again, not a new run.
test('a terminal-only replay does not create another stored result after reload', async () => {
  const original = setup(new MemoryRepository(), 'original-observation');
  original.history.start();
  original.finish();
  await settle();
  original.history.stop();
  original.session.dispose();

  const reopened = setup(original.repository, 'cached-terminal-replay');
  reopened.history.start();
  reopened.finish();
  await settle();

  assert.equal(reopened.repository.writes, 1);
  assert.equal(reopened.repository.values.size, 1);
  assert.equal(reopened.session.state.candidates.length, 0);
  assert.ok(reopened.repository.values.has('original-observation'));
  reopened.history.stop();
  reopened.session.dispose();
});

// A cached end can fill a prior recording's missing terminal sample or definition.
test('terminal evidence missing from an older archive is kept as a new observation', async () => {
  for (const missing of ['terminal sample', 'definition'] as const) {
    const collection = missing === 'terminal sample'
      ? new ResultsCollection({ byteBudget: 4096, terminalReserveBytes: 2048 })
      : new ResultsCollection();
    const frame = telemetryFrame();
    frame.simulation.status = 'finished';
    frame.simulation.payload = { receivedBytes: 1024, acknowledgedBytes: 1024 };
    collection.snapshot(frame, 100, true);
    const partial = assembleRunResult(collection.get('run-1')!, {
      id: 'older-partial', completion: { reason: 'finished', receivedAt: 100 },
    });
    assert.equal(partial.coverage.terminalSampleMissing, missing === 'terminal sample');

    const repository = new MemoryRepository();
    repository.values.set(partial.id, partial);
    const reopened = setup(repository, 'recovered-evidence');
    reopened.history.start();
    reopened.finish();
    await settle();

    assert.equal(repository.writes, 1, missing);
    assert.equal(repository.values.size, 2, missing);
    assert.ok(repository.values.has('older-partial'));
    assert.ok(repository.values.has('recovered-evidence'));
    reopened.history.stop();
    reopened.session.dispose();
  }
});
