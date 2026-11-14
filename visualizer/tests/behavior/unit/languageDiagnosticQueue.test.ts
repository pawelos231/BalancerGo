import assert from 'node:assert/strict';
import test from 'node:test';
import type { TestContext } from 'node:test';
import { LanguageDiagnosticQueue } from '../../../src/scenario-language/diagnosticQueue.ts';
import type { DiagnosticRequest, DiagnosticResponse } from '../../../src/scenario-language/diagnostics.ts';

function setup(t: TestContext) {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const sent: DiagnosticRequest[] = [];
  const published: { source: string; response: DiagnosticResponse }[] = [];
  const queue = new LanguageDiagnosticQueue(
    request => sent.push(request),
    (source, response) => published.push({ source, response }),
  );
  t.after(() => queue.dispose());
  return { queue, sent, published, tick: (milliseconds: number) => t.mock.timers.tick(milliseconds) };
}

test('language diagnostic queue sends at the 100 ms deadline and publishes its matching result', t => {
  const { queue, sent, published, tick } = setup(t);
  queue.check('source');

  tick(99);
  assert.deepEqual(sent, []);
  tick(1);
  assert.deepEqual(sent, [{ revision: 1, source: 'source' }]);

  const response = { revision: 1, diagnostic: null };
  queue.receive(response);
  assert.deepEqual(published, [{ source: 'source', response }]);
});

test('language diagnostic queue restarts the debounce for rapid edits', t => {
  const { queue, sent, tick } = setup(t);
  queue.check('first');
  tick(60);
  queue.check('second');
  tick(60);
  queue.check('latest');

  // The deadline belongs to the latest edit, even after earlier pauses have elapsed.
  tick(99);
  assert.deepEqual(sent, []);
  tick(1);
  assert.deepEqual(sent, [{ revision: 3, source: 'latest' }]);
});

test('language diagnostic queue skips obsolete drafts waiting behind a busy worker', t => {
  const { queue, sent, published, tick } = setup(t);
  queue.check('running');
  tick(100);

  // The worker stays busy while two newer drafts finish their debounce pauses.
  queue.check('obsolete waiting draft');
  tick(100);
  queue.check('latest waiting draft');
  tick(100);
  assert.deepEqual(sent, [{ revision: 1, source: 'running' }]);

  // Finishing the first compile sends only the newest waiting draft.
  queue.receive({ revision: 1, diagnostic: null });
  assert.deepEqual(sent, [
    { revision: 1, source: 'running' },
    { revision: 3, source: 'latest waiting draft' },
  ]);
  assert.deepEqual(published, []);

  const response = { revision: 3, diagnostic: null, failure: 'Compiler stopped.' };
  queue.receive(response);
  assert.deepEqual(published, [{ source: 'latest waiting draft', response }]);
});

test('language diagnostic queue ignores an old acknowledgment while a newer request runs', t => {
  const { queue, sent, published, tick } = setup(t);
  queue.check('first');
  tick(100);
  queue.check('second');
  tick(100);
  queue.receive({ revision: 1, diagnostic: null });

  // The second draft now runs, and the third waits for its reply.
  queue.check('third');
  tick(100);

  // Neither a duplicate old reply nor an unsent future reply frees the worker.
  queue.receive({ revision: 1, diagnostic: null });
  queue.receive({ revision: 3, diagnostic: null });
  assert.deepEqual(sent.map(request => request.revision), [1, 2]);
  assert.deepEqual(published, []);

  queue.receive({ revision: 2, diagnostic: null });
  assert.deepEqual(sent.map(request => request.revision), [1, 2, 3]);
  assert.deepEqual(published, []);
});

test('language diagnostic queue preserves a new edit deadline when the old reply arrives', t => {
  const { queue, sent, published, tick } = setup(t);
  queue.check('running');
  tick(100);
  queue.check('ready but obsolete');
  tick(100);

  // This edit replaces the ready draft but still needs its own typing pause.
  queue.check('new edit');
  tick(40);

  // The old reply arrives 40 ms into the new edit's 100 ms pause.
  queue.receive({ revision: 1, diagnostic: null });
  assert.deepEqual(sent.map(request => request.source), ['running']);
  assert.deepEqual(published, []);

  tick(59);
  assert.equal(sent.length, 1);
  tick(1);
  assert.deepEqual(sent, [
    { revision: 1, source: 'running' },
    { revision: 3, source: 'new edit' },
  ]);
});

test('language diagnostic queue disposal cancels timers and suppresses later requests and replies', t => {
  const { queue, sent, published, tick } = setup(t);
  queue.check('running');
  tick(100);
  queue.check('ready');
  tick(100);
  queue.check('debouncing');
  tick(50);

  // Dispose with a compile running and a newer edit halfway through its pause.
  queue.dispose();
  queue.dispose();

  queue.receive({ revision: 1, diagnostic: null });
  queue.check('after disposal');
  tick(1000);
  assert.deepEqual(sent, [{ revision: 1, source: 'running' }]);
  assert.deepEqual(published, []);
});
