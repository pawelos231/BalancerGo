import type { ReadyResultEntry } from './repository.ts';
import type { PublicationRange, ResultCoverage, RunResult } from './types.ts';

export type DisplayValue = number | string | boolean | null | undefined;

// Decimal counter strings never pass through Number or locale formatting.
export function exactValue(value: DisplayValue): string {
  if (value === null || value === undefined) return 'Unavailable';
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  return String(value);
}

export function approximateValue(value: number | string | null | undefined, unit = '', factor = 1): string {
  if (value === null || value === undefined) return 'Unavailable';
  const numeric = Number(value) * factor;
  if (!Number.isFinite(numeric)) return 'Unavailable';
  const formatted = new Intl.NumberFormat('en-US', { maximumSignificantDigits: 6 }).format(numeric);
  return `≈ ${formatted}${unit ? ` ${unit}` : ''}`;
}

export function clientTransferDurationValue(
  transfer: unknown,
  field: 'handshakeDurationSeconds' | 'payloadDurationSeconds',
): string {
  if (transfer === null || typeof transfer !== 'object' || Array.isArray(transfer)) return 'Unavailable';
  const source = transfer as Record<string, unknown>;
  const complete = field === 'handshakeDurationSeconds'
    ? source.handshakeCompleted === true : source.status === 'succeeded';
  const duration = source[field];
  if (!complete || typeof duration !== 'number' && typeof duration !== 'string') return 'Unavailable';
  return approximateValue(duration, 's');
}

export function resultDate(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return 'Unavailable';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unavailable';
  return date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' });
}

export function virtualRange(start: number | null, end: number | null): string {
  if (start === null && end === null) return 'Unavailable';
  return `${approximateValue(start)} – ${approximateValue(end)} virtual s`;
}

export function publicationRange(range: PublicationRange): string {
  return range.first === range.last ? range.first : `${range.first}–${range.last}`;
}

export function resultListEntry(result: RunResult): ReadyResultEntry {
  let start: number | null = null;
  let end: number | null = null;
  let algorithm: string | null = null;
  for (const sample of result.collection.samples) {
    start = start === null ? sample.virtualTimeSeconds : Math.min(start, sample.virtualTimeSeconds);
    end = end === null ? sample.virtualTimeSeconds : Math.max(end, sample.virtualTimeSeconds);
    if (sample.publicationSequence === result.summary.observation?.publicationSequence) {
      algorithm = sample.snapshot.algorithm;
    }
  }
  return {
    status: 'ready', id: result.id, runId: result.runId, resultVersion: result.resultVersion,
    receivedAt: result.completion.receivedAt, completionReason: result.completion.reason,
    complete: result.coverage.complete, sampleCount: result.collection.samples.length,
    virtualTimeStartSeconds: start, virtualTimeEndSeconds: end,
    seed: result.context.seed, model: result.context.experiment?.model ?? null, algorithm,
  };
}

export function coverageNotes(coverage: ResultCoverage): string[] {
  const notes: string[] = [];
  if (coverage.joinedAfterStart) notes.push('Observation joined after the run started.');
  if (coverage.publicationsBeforeFirstReceipt) {
    notes.push(`Publications before the first receipt: ${publicationRange(coverage.publicationsBeforeFirstReceipt)}.`);
  }
  if (coverage.unrecordedPublicationCount !== '0') {
    notes.push(`Unrecorded publications: ${coverage.unrecordedPublicationCount}.`);
  }
  if (coverage.disconnections) notes.push(`Connection interruptions: ${coverage.disconnections}.`);
  if (coverage.missingDefinitionRevisions.length) {
    notes.push(`Missing topology definitions: ${coverage.missingDefinitionRevisions.length} revision(s).`);
  }
  if (coverage.missingScenario) notes.push('The executed scenario is missing.');
  if (coverage.missingAuthoritativeScenario) notes.push('An authoritative backend scenario was not received.');
  if (coverage.missingExperiment) notes.push('The experiment manifest is missing.');
  if (coverage.metadataAfterFirstSample) notes.push('Scenario metadata arrived after the first sample.');
  if (coverage.terminalSampleMissing) notes.push('The terminal sample is missing; summary uses the last observation.');
  if (coverage.memoryLimited) notes.push('The collection memory budget was reached.');
  const limits = coverage.limits;
  for (const [label, count] of [
    ['samples', limits.droppedSamples], ['definitions', limits.droppedDefinitions],
    ['metadata deliveries', limits.droppedMetadata], ['observation markers', limits.droppedMarkers],
  ] as const) {
    if (count) notes.push(`Rejected ${label}: ${count}.`);
  }
  if (limits.markerLimitReached) notes.push('The observation marker limit was reached.');
  if (limits.terminalSnapshotDropped) notes.push('The terminal snapshot could not be retained.');
  return notes;
}

export function sourceExecutionError(result: RunResult): string | null {
  const sequence = result.summary.observation?.publicationSequence;
  const source = result.collection.samples.find(sample => sample.publicationSequence === sequence);
  return source?.snapshot.simulation.error ?? null;
}
