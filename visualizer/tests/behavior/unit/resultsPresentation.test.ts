import assert from 'node:assert/strict';
import test from 'node:test';
import { assembleRunResult } from '../../../src/results/assembly.ts';
import { ResultsCollection } from '../../../src/results/collection.ts';
import {
  approximateValue, clientTransferDurationValue, coverageNotes, exactValue, publicationRange, resultDate,
  resultListEntry, sourceExecutionError, virtualRange,
} from '../../../src/results/presentation.ts';
import { scenarioFixture, experimentFixture } from '../../scenarioFixture.ts';
import { telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

function collectedResult() {
  const collection = new ResultsCollection();
  collection.metadata({
    runId: 'run-1', status: 'running', scenario: scenarioFixture(), experiment: experimentFixture(),
  }, 10);
  collection.definition(topologyDefinition(), 11);
  for (const [sequence, virtualTime] of [['2', 4], ['1', 0], ['3', 2]] as const) {
    const frame = telemetryFrame('1', 'run-1', 1, sequence);
    frame.simulation.virtualTimeSeconds = virtualTime;
    frame.algorithm = sequence === '3' ? 'terminal-algorithm' : 'earlier-algorithm';
    collection.snapshot(frame, 20, sequence === '3');
  }
  return assembleRunResult(collection.get('run-1')!, {
    id: 'local-1', completion: { reason: 'stopped', receivedAt: 40 },
  });
}

test('display preserves counter, seed and publication digits beyond JavaScript precision', () => {
  assert.equal(exactValue('18446744073709551615'), '18446744073709551615');
  assert.equal(exactValue('-9007199254740993'), '-9007199254740993');
  assert.equal(publicationRange({ first: '9007199254740993', last: '9007199254740995' }),
    '9007199254740993–9007199254740995');
  assert.equal(publicationRange({ first: '18446744073709551615', last: '18446744073709551615' }),
    '18446744073709551615');
  assert.match(approximateValue('18446744073709551615', 'B / virtual s'), /^≈ .* B \/ virtual s$/);
});

test('unavailable values remain distinct from measured zero, false and very short durations', () => {
  assert.equal(exactValue(null), 'Unavailable');
  assert.equal(exactValue(undefined), 'Unavailable');
  assert.equal(exactValue(0), '0');
  assert.equal(exactValue(false), 'No');
  assert.equal(approximateValue(null), 'Unavailable');
  assert.equal(approximateValue(0, 's'), '≈ 0 s');
  assert.equal(approximateValue(1e-9, 's'), '≈ 0.000000001 s');
  assert.equal(approximateValue(Infinity), 'Unavailable');
  assert.equal(virtualRange(null, null), 'Unavailable');
  assert.equal(virtualRange(0, 2), '≈ 0 – ≈ 2 virtual s');
  assert.equal(resultDate(null), 'Unavailable');
  assert.equal(resultDate(NaN), 'Unavailable');
});

test('client durations require completion evidence while successful zero remains a measurement', () => {
  for (const status of ['failed', 'inProgress']) {
    const pending = {
      status, handshakeCompleted: false, handshakeDurationSeconds: 0, payloadDurationSeconds: 0,
    };
    assert.equal(clientTransferDurationValue(pending, 'handshakeDurationSeconds'), 'Unavailable');
    assert.equal(clientTransferDurationValue(pending, 'payloadDurationSeconds'), 'Unavailable');

    const handshakeComplete = { ...pending, handshakeCompleted: true };
    assert.equal(clientTransferDurationValue(handshakeComplete, 'handshakeDurationSeconds'), '≈ 0 s');
    assert.equal(clientTransferDurationValue(handshakeComplete, 'payloadDurationSeconds'), 'Unavailable');
  }
  const succeeded = {
    status: 'succeeded', handshakeCompleted: true, handshakeDurationSeconds: 0, payloadDurationSeconds: 0,
  };
  assert.equal(clientTransferDurationValue(succeeded, 'handshakeDurationSeconds'), '≈ 0 s');
  assert.equal(clientTransferDurationValue(succeeded, 'payloadDurationSeconds'), '≈ 0 s');
});

test('list rows use all retained virtual bounds and the selected summary publication algorithm', () => {
  const result = collectedResult();
  const before = JSON.stringify(result);
  const row = resultListEntry(result);
  assert.equal(row.virtualTimeStartSeconds, 0);
  assert.equal(row.virtualTimeEndSeconds, 4);
  assert.equal(row.algorithm, 'terminal-algorithm');
  assert.equal(row.sampleCount, 3);
  assert.equal(row.seed, '-9007199254740993');
  assert.equal(row.completionReason, 'stopped');
  assert.equal(JSON.stringify(result), before);
});

test('coverage display exposes metadata, budget and observation losses without rounding gap counts', () => {
  const result = collectedResult();
  const coverage = {
    ...result.coverage, complete: false, joinedAfterStart: true,
    publicationsBeforeFirstReceipt: { first: '1', last: '9007199254740993' },
    unrecordedPublicationCount: '9007199254740993', disconnections: 2,
    missingDefinitionRevisions: ['missing'], missingScenario: true,
    missingAuthoritativeScenario: true, missingExperiment: true,
    metadataAfterFirstSample: true, terminalSampleMissing: true, memoryLimited: true,
    limits: {
      memoryLimited: true, droppedSamples: 2, droppedDefinitions: 3, droppedMetadata: 4,
      droppedMarkers: 5, markerLimitReached: true, terminalSnapshotDropped: true,
    },
  };
  const text = coverageNotes(coverage).join('\n');
  for (const expected of [
    '9007199254740993', 'interruptions: 2', 'definitions: 1', 'scenario is missing',
    'authoritative backend', 'manifest is missing', 'after the first sample', 'terminal sample',
    'memory budget', 'Rejected samples: 2', 'Rejected definitions: 3',
    'Rejected metadata deliveries: 4', 'Rejected observation markers: 5', 'marker limit',
    'terminal snapshot',
  ]) assert.ok(text.includes(expected), expected);
});

test('a manually saved failed source exposes its summary execution error', () => {
  const collection = new ResultsCollection();
  const failed = telemetryFrame();
  failed.simulation.status = 'failed';
  failed.simulation.error = 'event budget exhausted';
  collection.snapshot(failed, 1, true);
  const result = assembleRunResult(collection.get('run-1')!, {
    id: 'failed-local', completion: { reason: 'stopped', receivedAt: 2 },
  });
  assert.equal(sourceExecutionError(result), 'event budget exhausted');
  assert.equal(sourceExecutionError(collectedResult()), null);
});
