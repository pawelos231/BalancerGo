import assert from 'node:assert/strict';
import test from 'node:test';
import { httpStartAcknowledged } from '../src/scenarios/startAcknowledgement.ts';

test('a matching live or already finished snapshot acknowledges HTTP Start', () => {
  for (const status of ['running', 'finished', 'failed'] as const) {
    assert.equal(httpStartAcknowledged(
      'run-1',
      { runId: 'run-1', status },
      { runId: 'run-1', status },
    ), true);
  }
});

test('an old idle frame cannot acknowledge a still accepted run', () => {
  assert.equal(httpStartAcknowledged(
    'run-1',
    { runId: 'run-1', status: 'accepted' },
    { status: 'idle' },
  ), false);
  assert.equal(httpStartAcknowledged(
    'run-1',
    { runId: 'run-1', status: 'running' },
    { runId: 'older-run', status: 'finished' },
  ), false);
});

test('Stop can clear an accepted run before its WebSocket frame is delivered', () => {
  assert.equal(httpStartAcknowledged('run-1', null, { status: 'idle' }), true);
  assert.equal(httpStartAcknowledged(
    'run-1', null, { runId: 'older-run', status: 'finished' },
  ), false);
});

test('a later accepted run supersedes the pending run', () => {
  assert.equal(httpStartAcknowledged(
    'run-1',
    { runId: 'run-2', status: 'accepted' },
    { status: 'idle' },
  ), true);
});
