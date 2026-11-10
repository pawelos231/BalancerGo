import assert from 'node:assert/strict';
import test from 'node:test';
import {
  LocalResultsRepository, RepositoryError, RESULTS_INDEX_KEY, RESULTS_STORAGE_PREFIX,
  resultStorageKey, STORAGE_VERSION, type ResultsStorage,
  deletedRunStorageKey, DELETED_RUN_LIMIT,
} from '../../../src/results/repository.ts';
import { parseRunResultJSON, ResultValidationError } from '../../../src/results/validation.ts';
import type { RunResult } from '../../../src/results/types.ts';
import { resultFixture } from '../../resultFixture.ts';

class MemoryStorage implements ResultsStorage {
  readonly values = new Map<string, string>();
  readonly reads: string[] = [];
  onSet: ((key: string, value: string) => void) | null = null;
  onRemove: ((key: string) => void) | null = null;
  get length() { return this.values.size; }
  key(index: number) { return [...this.values.keys()][index] ?? null; }
  getItem(key: string) { this.reads.push(key); return this.values.get(key) ?? null; }
  setItem(key: string, value: string) { this.onSet?.(key, value); this.values.set(key, value); }
  removeItem(key: string) { this.onRemove?.(key); this.values.delete(key); }
}

function expectError(action: () => unknown, code: RepositoryError['code'], durableRecord = false): RepositoryError {
  try { action(); } catch (error) {
    assert.ok(error instanceof RepositoryError);
    assert.equal(error.code, code);
    assert.equal(error.durableRecord, durableRecord);
    return error;
  }
  assert.fail(`Expected ${code}`);
}

function storeRecord(storage: MemoryStorage, result: RunResult) {
  storage.values.set(resultStorageKey(result.id), JSON.stringify({ storageVersion: STORAGE_VERSION, result }));
}

function changeRecord(storage: MemoryStorage, id: string, change: (value: Record<string, unknown>) => void) {
  const value = JSON.parse(storage.values.get(resultStorageKey(id))!);
  change(value);
  storage.values.set(resultStorageKey(id), JSON.stringify(value));
}

test('archives round-trip all fields, exact integers, unsafe float settings and unknown JSON fields', () => {
  const storage = new MemoryStorage();
  const result = JSON.parse(JSON.stringify(resultFixture('local / 中文')));
  result.extra = { source: 'test', integerText: '18446744073709551615' };
  result.collection.samples[0].snapshot.extra = JSON.parse('{"__proto__":{"retained":true}}');
  const repository = new LocalResultsRepository(storage);
  const row = repository.save(result);
  assert.equal(row.status, 'ready');
  assert.equal(row.sampleCount, 1);
  assert.equal(row.virtualTimeStartSeconds, 0.1);
  assert.equal(row.virtualTimeEndSeconds, 0.1);
  assert.equal(row.seed, '-9007199254740993');
  assert.equal(row.model, 'test-model-v1');
  assert.equal(row.algorithm, 'RoundRobin');
  const loaded = new LocalResultsRepository(storage).read(result.id)!;
  assert.deepEqual(loaded, result);
  assert.equal(loaded.summary.payload!.receivedBytes, '18446744073709551615');
  assert.equal(loaded.summary.performance!.eventsExecuted, '9007199254740993');
  assert.equal(loaded.context.scenario!.fixed.execution.speed, '10000000000000000');
  assert.equal(loaded.context.experiment!.execution.speed, '10000000000000000');
  assert.ok(Object.isFrozen(loaded));
  assert.ok(Object.isFrozen(loaded.collection.samples[0].snapshot.servers[0].clients));
  assert.throws(() => { loaded.collection.samples[0].snapshot.servers[0].clients.push({ id: 'bad', retrySyn: 0, retryData: 0 }); }, TypeError);
  assert.deepEqual(repository.list(), { entries: [row], warnings: [] });
});

test('the compact list reads only the index and owned key names, never archived series', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  storeRecord(storage, resultFixture('orphan'));
  storage.values.set('unrelated', '{not json');
  storage.values.set(`${RESULTS_STORAGE_PREFIX}other`, 'unrelated');
  storage.values.set(`${RESULTS_STORAGE_PREFIX}record:%zz`, 'malformed suffix');
  storage.reads.length = 0;
  const listing = repository.list();
  assert.deepEqual(listing.entries.map(({ id, status }) => ({ id, status })), [
    { id: 'a', status: 'ready' }, { id: 'orphan', status: 'recoverable' },
  ]);
  assert.deepEqual(storage.reads, [RESULTS_INDEX_KEY]);
  assert.ok(Object.isFrozen(listing.entries[0]));
});

test('native backend omitempty fields remain omitted after archiving', () => {
  const result = JSON.parse(JSON.stringify(resultFixture('a')));
  for (const scenario of [result.context.scenario, result.collection.metadata.scenario]) {
    delete scenario.fixed.network.profiles;
    for (const state of Object.values(scenario.states) as Record<string, unknown>[]) {
      const resources = state.resources as { backends: { network: { profile?: string } }[] };
      for (const backend of resources.backends) delete backend.network.profile;
      const workload = state.workload as { clientGroups: { network: { profile?: string }; overrides: { network: { profile?: string } }[] }[] };
      for (const group of workload.clientGroups) {
        delete group.network.profile;
        for (const override of group.overrides) delete override.network.profile;
      }
    }
  }
  const repository = new LocalResultsRepository(new MemoryStorage());
  repository.save(result);
  assert.deepEqual(repository.read('a'), result);
});

test('same-ID identical JSON is idempotent regardless of key order; conflicting payload never overwrites', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  const result = resultFixture('a');
  repository.save(result);
  const saved = storage.values.get(resultStorageKey('a'));
  const reordered = Object.fromEntries(Object.entries(result).reverse()) as unknown as RunResult;
  repository.save(reordered);
  assert.equal(storage.values.get(resultStorageKey('a')), saved);
  const changed = { ...result, completion: { ...result.completion, receivedAt: 999 } };
  expectError(() => repository.save(changed), 'conflict');
  assert.equal(storage.values.get(resultStorageKey('a')), saved);
  assert.equal(repository.list().entries.length, 1);
});

test('separate browser observations of one run retain independent local IDs', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('first-observer', 'same-run'));
  repository.save(resultFixture('second-observer', 'same-run'));
  assert.equal(repository.list().entries.length, 2);
  assert.equal(repository.read('first-observer')!.runId, repository.read('second-observer')!.runId);
});

test('quota failure before the record write preserves all existing records and index', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  const before = new Map(storage.values);
  storage.onSet = () => { throw new DOMException('full', 'QuotaExceededError'); };
  expectError(() => repository.save(resultFixture('b')), 'quota');
  assert.deepEqual(storage.values, before);
});

test('index failure leaves the full record durable and recoverable without rolling it back', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  const candidate = resultFixture('b');
  storage.onSet = (key) => { if (key === RESULTS_INDEX_KEY) throw new DOMException('full', 'QuotaExceededError'); };
  expectError(() => repository.save(candidate), 'quota', true);
  assert.equal(storage.values.has(resultStorageKey('b')), true);
  assert.equal(repository.list().entries.find((entry) => entry.id === 'b')!.status, 'recoverable');
  assert.deepEqual(repository.read('b'), candidate);
  storage.onSet = null;
  assert.deepEqual(new LocalResultsRepository(storage).recover('b'), candidate);
  assert.equal(repository.list().entries.find((entry) => entry.id === 'b')!.status, 'ready');
});

test('unavailable storage and SecurityError are typed and do not clear durable records', () => {
  const storage = new MemoryStorage();
  new LocalResultsRepository(storage).save(resultFixture('a'));
  const before = new Map(storage.values);
  const denied = new LocalResultsRepository(() => { throw new DOMException('denied', 'SecurityError'); });
  for (const action of [() => denied.list(), () => denied.read('a'), () => denied.save(resultFixture('b')), () => denied.remove('a'), () => denied.recover('a')]) {
    expectError(action, 'unavailable');
  }
  assert.deepEqual(storage.values, before);
});

test('a corrupt index exposes record IDs and selected recovery rebuilds only its own row', () => {
  const storage = new MemoryStorage();
  storeRecord(storage, resultFixture('a'));
  storeRecord(storage, resultFixture('b'));
  storage.values.set(RESULTS_INDEX_KEY, '{damaged');
  const repository = new LocalResultsRepository(storage);
  assert.equal(repository.list().warnings.length, 1);
  assert.deepEqual(repository.list().entries.map((entry) => entry.status), ['recoverable', 'recoverable']);
  storage.reads.length = 0;
  repository.recover('a');
  assert.equal(storage.reads.includes(resultStorageKey('b')), false);
  assert.deepEqual(repository.list().entries.map(({ id, status }) => ({ id, status })), [
    { id: 'a', status: 'ready' }, { id: 'b', status: 'recoverable' },
  ]);
  assert.equal(repository.read('b')!.id, 'b');
});

test('individual damaged index rows do not hide other ready records or their own record IDs', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  repository.save(resultFixture('b'));
  const index = JSON.parse(storage.values.get(RESULTS_INDEX_KEY)!);
  delete index.entries[0].sampleCount;
  storage.values.set(RESULTS_INDEX_KEY, JSON.stringify(index));
  const listing = repository.list();
  assert.equal(listing.warnings.length, 1);
  assert.equal(listing.entries.find((entry) => entry.id === 'a')!.status, 'recoverable');
  assert.equal(listing.entries.find((entry) => entry.id === 'b')!.status, 'ready');
});

test('a corrupt record has a local readable error and never destroys a healthy neighbor', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  repository.save(resultFixture('b'));
  storage.values.set(resultStorageKey('a'), '{broken');
  const before = new Map(storage.values);
  expectError(() => repository.read('a'), 'corrupt');
  expectError(() => repository.recover('a'), 'corrupt');
  expectError(() => repository.save(resultFixture('a')), 'invalid');
  assert.deepEqual(storage.values, before);
  assert.equal(repository.read('b')!.id, 'b');
});

test('storage, result and collection future versions are preserved and reported', () => {
  for (const target of ['storage', 'result', 'collection']) {
    const storage = new MemoryStorage();
    storeRecord(storage, resultFixture('a'));
    changeRecord(storage, 'a', (envelope) => {
      const result = envelope.result as Record<string, unknown>;
      if (target === 'storage') envelope.storageVersion = 99;
      if (target === 'result') result.resultVersion = 99;
      if (target === 'collection') (result.collection as Record<string, unknown>).collectionVersion = 99;
    });
    const before = new Map(storage.values);
    const repository = new LocalResultsRepository(storage);
    expectError(() => repository.read('a'), 'unsupported-version');
    expectError(() => repository.save(resultFixture('a')), 'unsupported-version');
    assert.deepEqual(storage.values, before);
  }
});

test('an unsupported future index is never overwritten, including after a durable record save', () => {
  const storage = new MemoryStorage();
  storage.values.set(RESULTS_INDEX_KEY, JSON.stringify({ storageVersion: 99, entries: ['future'] }));
  const originalIndex = storage.values.get(RESULTS_INDEX_KEY);
  const repository = new LocalResultsRepository(storage);
  expectError(() => repository.save(resultFixture('a')), 'unsupported-version', true);
  assert.equal(storage.values.get(RESULTS_INDEX_KEY), originalIndex);
  assert.equal(repository.list().entries[0].status, 'recoverable');
  assert.equal(repository.read('a')!.id, 'a');
  const recovery = expectError(() => repository.recover('a'), 'unsupported-version', true);
  assert.equal(recovery.operation, 'recover');
  assert.equal(storage.values.get(RESULTS_INDEX_KEY), originalIndex);
});

test('record key identity, run identity and publication identity must agree', () => {
  const changes = [
    (result: RunResult) => { (result as { id: string }).id = 'another-id'; },
    (result: RunResult) => { (result.collection as { runId: string }).runId = 'other-run'; },
    (result: RunResult) => { result.collection.definitions[0].runId = 'other-run'; },
    (result: RunResult) => { result.collection.samples[0].snapshot.simulation.runId = 'other-run'; },
    (result: RunResult) => { result.collection.samples[0].snapshot.simulation.publicationSequence = '2'; },
    (result: RunResult) => { (result.collection.samples[0] as { publicationSequence: string }).publicationSequence = '18446744073709551616'; },
    (result: RunResult) => { (result.summary.observation as { publicationSequence: string }).publicationSequence = '2'; },
  ];
  for (const change of changes) {
    const storage = new MemoryStorage();
    const result = JSON.parse(JSON.stringify(resultFixture('a'))) as RunResult;
    change(result);
    storeRecord(storage, result);
    // Exercise key/payload mismatch using the originally requested key.
    if (result.id !== 'a') storage.values.set(resultStorageKey('a'), storage.values.get(resultStorageKey(result.id))!);
    expectError(() => new LocalResultsRepository(storage).read('a'), 'corrupt');
  }
});

test('required containers, resource shapes, timestamps, scalars and units are checked', () => {
  const changes: ((result: Record<string, unknown>) => void)[] = [
    (result) => { delete result.summary; },
    (result) => { (result.completion as Record<string, unknown>).receivedAt = 'yesterday'; },
    (result) => { ((result.context as Record<string, unknown>).publicationInterval as Record<string, unknown>).clock = 'virtual'; },
    (result) => { const data = result as unknown as RunResult; (data.collection.samples[0].snapshot.topology.nodes[0] as unknown as Record<string, unknown>).metrics = []; },
    (result) => { const data = result as unknown as RunResult; (data.collection.samples[0].snapshot.servers[0] as unknown as Record<string, unknown>).clients = {}; },
    (result) => { const data = result as unknown as RunResult; data.collection.samples[0].snapshot.topology.links[0].stats.deliveredBytes = 'many'; },
    (result) => { const data = result as unknown as RunResult; (data.summary.resources as unknown as Record<string, unknown>).nodes = null; },
    (result) => { const data = result as unknown as RunResult; (data.context.scenario!.fixed as unknown as Record<string, unknown>).network = null; },
  ];
  for (const change of changes) {
    const storage = new MemoryStorage();
    const result = JSON.parse(JSON.stringify(resultFixture('a')));
    change(result);
    storeRecord(storage, result);
    expectError(() => new LocalResultsRepository(storage).read('a'), 'corrupt');
  }
});

test('size budgets are checked before JSON parsing, including UTF-8 multibyte content', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage, { recordByteLimit: 16 });
  storage.values.set(resultStorageKey('a'), 'not-json'.repeat(3));
  expectError(() => repository.read('a'), 'too-large');
  assert.throws(() => parseRunResultJSON('"中文中文"', undefined, 8), (error) => error instanceof ResultValidationError && error.code === 'too-large');
  const before = new Map(storage.values);
  expectError(() => repository.save(resultFixture('b')), 'too-large');
  assert.deepEqual(storage.values, before);
});

test('record-first save retains a recoverable orphan when the compact index exceeds its budget', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage, { indexByteLimit: 16 });
  expectError(() => repository.save(resultFixture('a')), 'too-large', true);
  assert.equal(repository.list().entries[0].status, 'recoverable');
  assert.equal(repository.read('a')!.id, 'a');
});

test('removal filters stale rows and never clears unrelated keys, even when index update fails', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  repository.save(resultFixture('b'));
  storage.values.set('unrelated', 'keep');
  storage.onSet = (key) => { if (key === RESULTS_INDEX_KEY) throw new DOMException('full', 'QuotaExceededError'); };
  expectError(() => repository.remove('a'), 'quota');
  assert.deepEqual(repository.list().entries.map((entry) => entry.id), ['b']);
  assert.equal(repository.read('a'), null);
  assert.equal(storage.values.get('unrelated'), 'keep');
  storage.onSet = null;
  repository.remove('missing');
  assert.deepEqual(repository.list().entries.map((entry) => entry.id), ['b']);
  expectError(() => repository.recover('missing'), 'not-found');
});

test('a failed record deletion preserves its record and all existing index rows', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  const before = new Map(storage.values);
  storage.onRemove = () => { throw new DOMException('denied', 'SecurityError'); };
  expectError(() => repository.remove('a'), 'unavailable');
  for (const [key, value] of before) assert.equal(storage.values.get(key), value);
  assert.equal(repository.isDeletedRun('run-1'), true);
});

test('two repository instances fresh-read the index instead of replacing earlier saved rows', () => {
  const storage = new MemoryStorage();
  const first = new LocalResultsRepository(storage);
  const second = new LocalResultsRepository(storage);
  first.list();
  second.list();
  first.save(resultFixture('a'));
  second.save(resultFixture('b'));
  assert.deepEqual(first.list().entries.map((entry) => entry.id), ['a', 'b']);
  second.remove('a');
  first.save(resultFixture('c'));
  assert.deepEqual(second.list().entries.map((entry) => entry.id), ['b', 'c']);
});

test('a deliberately interleaved index write cannot lose durable records and exposes the orphan', () => {
  const storage = new MemoryStorage();
  const first = new LocalResultsRepository(storage);
  const second = new LocalResultsRepository(storage);
  storage.onSet = (key) => {
    if (key !== RESULTS_INDEX_KEY) return;
    storage.onSet = null;
    second.save(resultFixture('b'));
  };
  first.save(resultFixture('a'));
  assert.equal(first.read('a')!.id, 'a');
  assert.equal(first.read('b')!.id, 'b');
  assert.equal(first.list().entries.find((entry) => entry.id === 'b')!.status, 'recoverable');
  second.recover('b');
  assert.ok(first.list().entries.every((entry) => entry.status === 'ready'));
});

test('the JSON archive boundary rejects non-JSON values instead of silently altering fields', () => {
  const result = JSON.parse(JSON.stringify(resultFixture('a')));
  result.unknown = undefined;
  const storage = new MemoryStorage();
  expectError(() => new LocalResultsRepository(storage).save(result), 'invalid');
  assert.equal(storage.length, 0);
  delete result.unknown;
  result.context.seed = Infinity;
  expectError(() => new LocalResultsRepository(storage).save(result), 'invalid');
  result.context.seed = 1;
  delete result.collection.samples[0];
  expectError(() => new LocalResultsRepository(storage).save(result), 'invalid');
});

test('raw storage rejects unsafe numeric counters and seeds without rewriting the record', () => {
  const changes: ((result: RunResult) => void)[] = [
    (result) => { result.summary.payload!.receivedBytes = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { (result.context as { seed: string }).seed = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { result.context.scenario!.fixed.execution.seed = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { result.context.scenario!.fixed.network.defaults.bandwidthBitsPerSecond = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { result.context.scenario!.fixed.tcp.mssBytes = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { result.collection.samples[0].snapshot.topology.nodes[0].metrics.completedStreams = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { result.collection.definitions[0].nodes.find((node) => node.id === 'client-a')!.metrics[0].value = 'UNSAFE_INTEGER_LITERAL'; },
    (result) => { result.collection.definitions[0].nodes[0].metrics.push({ key: 'futureByteCounter', label: 'Future bytes', unit: 'bytes', value: 'UNSAFE_INTEGER_LITERAL' }); },
  ];
  for (const change of changes) {
    const result = JSON.parse(JSON.stringify(resultFixture('a'))) as RunResult;
    change(result);
    const storage = new MemoryStorage();
    const raw = JSON.stringify({ storageVersion: STORAGE_VERSION, result })
      .replace('"UNSAFE_INTEGER_LITERAL"', '18446744073709551615');
    storage.values.set(resultStorageKey('a'), raw);
    const repository = new LocalResultsRepository(storage);
    expectError(() => repository.read('a'), 'corrupt');
    expectError(() => repository.recover('a'), 'corrupt');
    assert.equal(storage.values.get(resultStorageKey('a')), raw);
    assert.equal(storage.values.has(RESULTS_INDEX_KEY), false);
  }
});

test('large integer-valued measured floats retain their numeric type and unknown fields', () => {
  const result = JSON.parse(JSON.stringify(resultFixture('a')));
  result.collection.samples[0].snapshot.topology.nodes[0].metrics.hostPacketProcessingTimeUs = 1e16;
  result.collection.samples[0].snapshot.topology.nodes[0].metrics.throughput = 1e18;
  result.collection.definitions[0].nodes[0].metrics.push(
    { key: 'hostPacketProcessingTimeUs', label: 'Processing time', unit: 'µs', value: 1e16 },
    { key: 'throughput', label: 'Throughput', unit: 'bit/s', value: 1e18 },
  );
  result.summary.performance.eventsPerWallSecond = 1e19;
  result.extra = { approximate: 1e20, exactText: '18446744073709551615' };
  const storage = new MemoryStorage();
  storeRecord(storage, result);
  const repository = new LocalResultsRepository(storage);
  const loaded = repository.read('a')!;
  assert.deepEqual(loaded, result);
  assert.equal(typeof loaded.collection.samples[0].snapshot.topology.nodes[0].metrics.hostPacketProcessingTimeUs, 'number');
  assert.equal(typeof loaded.collection.definitions[0].nodes[0].metrics[1].value, 'number');
  assert.deepEqual(repository.recover('a'), result);
});

test('an unsafe numeric seed in the compact index becomes recoverable instead of rounded metadata', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  const index = JSON.parse(storage.values.get(RESULTS_INDEX_KEY)!);
  index.entries[0].seed = 18446744073709552000;
  storage.values.set(RESULTS_INDEX_KEY, JSON.stringify(index));
  assert.equal(repository.list().entries[0].status, 'recoverable');
  repository.recover('a');
  const row = repository.list().entries[0];
  assert.equal(row.status, 'ready');
  if (row.status === 'ready') assert.equal(row.seed, '-9007199254740993');
});

// A retained backend end must not recreate a result the user already deleted.
test('deletion remembers the run across repository instances without listing markers as results', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a', 'run-a'));
  repository.save(resultFixture('b', 'run-b'));
  repository.remove('a');

  const reopened = new LocalResultsRepository(storage);
  assert.equal(reopened.isDeletedRun('run-a'), true);
  assert.equal(reopened.isDeletedRun('run-b'), false);
  assert.equal(reopened.read('a'), null);
  storage.reads.length = 0;
  assert.deepEqual(reopened.list().entries.map(entry => entry.id), ['b']);
  assert.deepEqual(storage.reads, [RESULTS_INDEX_KEY]);
});

// If dismissal cannot be stored, deletion stops before removing the only record.
test('a failed deletion marker write leaves the saved result and index intact', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  const before = new Map(storage.values);
  storage.onSet = (key) => {
    if (key === deletedRunStorageKey('run-1')) throw new DOMException('full', 'QuotaExceededError');
  };

  expectError(() => repository.remove('a'), 'quota');
  assert.deepEqual(storage.values, before);
  assert.equal(repository.isDeletedRun('run-1'), false);
});

test('a damaged result can use its valid index identity for persistent deletion', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  storage.values.set(resultStorageKey('a'), '{broken');
  repository.remove('a');
  assert.equal(repository.isDeletedRun('run-1'), true);
  assert.equal(repository.read('a'), null);
});

// Cleanup keeps the newest dismissal and does not destroy unknown marker versions.
test('deleted-run markers are bounded and unsupported markers remain untouched', () => {
  const storage = new MemoryStorage();
  for (let index = 0; index < DELETED_RUN_LIMIT; index++) {
    storage.values.set(deletedRunStorageKey(`old-${index}`), JSON.stringify({
      storageVersion: STORAGE_VERSION, deletedAt: index,
    }));
  }
  const futureKey = deletedRunStorageKey('future-run');
  storage.values.set(futureKey, '{"storageVersion":99,"deletedAt":0}');
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a'));
  repository.remove('a');

  assert.equal(repository.isDeletedRun('run-1'), true);
  assert.equal(repository.isDeletedRun('old-0'), false);
  assert.equal(storage.values.get(futureKey), '{"storageVersion":99,"deletedAt":0}');
  expectError(() => repository.isDeletedRun('future-run'), 'unsupported-version');
  const markerKeys = [...storage.values.keys()].filter(key => key.includes(':deleted:'));
  assert.equal(markerKeys.length, DELETED_RUN_LIMIT + 1);
});

test('deleting a result cannot downgrade its existing future deletion marker', () => {
  const storage = new MemoryStorage();
  const repository = new LocalResultsRepository(storage);
  repository.save(resultFixture('a', 'run-a'));
  storage.values.set(deletedRunStorageKey('run-a'), '{"storageVersion":99,"futurePolicy":"keep"}');
  const before = new Map(storage.values);

  expectError(() => repository.remove('a'), 'unsupported-version');
  assert.deepEqual(storage.values, before);
  assert.equal(repository.read('a')!.runId, 'run-a');
});
