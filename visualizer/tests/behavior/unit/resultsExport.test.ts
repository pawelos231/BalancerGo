import assert from 'node:assert/strict';
import test from 'node:test';
import { createResultJSONExport, resultExportFilename } from '../../../src/results/export.ts';
import { resultFixture } from '../../resultFixture.ts';

test('JSON export round-trips the whole result without changing exact counters or unknown fields', async () => {
  const result = {
    ...resultFixture(),
    futureField: { label: '=1+1', nested: [null, {}, [], '18446744073709551615'] },
  };
  const before = JSON.stringify(result);
  const file = createResultJSONExport(result);
  const contents = await file.blob.text();
  const restored = JSON.parse(contents);

  // This is a result archive, not the smaller scenario document.
  assert.deepEqual(restored, result);
  assert.equal(restored.resultVersion, 1);
  assert.equal(restored.collection.samples[0].snapshot.simulation.performance.eventsExecuted, '9007199254740993');
  assert.equal(restored.summary.payload.receivedBytes, '18446744073709551615');
  assert.equal(JSON.stringify(result), before);
  assert.equal(file.blob.type, 'application/json;charset=utf-8');
  assert.ok(contents.endsWith('\n'));
});

test('result filenames use bounded local IDs without path separators or reserved filename characters', () => {
  const result = resultFixture('../unsafe:*?"<>| result', 'run/\\' + 'a'.repeat(200));
  const filename = resultExportFilename(result, 'json');

  assert.ok(filename.startsWith('load-balancer-result-run--'));
  assert.ok(filename.endsWith('.json'));
  assert.ok(filename.length <= 188);
  assert.doesNotMatch(filename, /[\\/:*?"<>|]/);
  assert.ok(resultExportFilename(result, 'xlsx').endsWith('.xlsx'));
});

test('an unsaved candidate exports directly without any storage or network access', async () => {
  const candidate = resultFixture('ram-only');
  const file = createResultJSONExport(candidate);

  // The same immutable value can be saved later or exported immediately.
  assert.equal(JSON.parse(await file.blob.text()).id, candidate.id);
  assert.equal(candidate.collection.samples.length, 1);
  assert.ok(Object.isFrozen(candidate));
});
