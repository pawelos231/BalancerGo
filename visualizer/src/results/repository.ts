import { freezeTree } from './immutable.ts';
import type { RunResult } from './types.ts';
import { requireJSONSize, RESULT_RECORD_BYTE_LIMIT, ResultValidationError, validateRunResult } from './validation.ts';

// Storage envelopes evolve independently from the public RunResult document.
export const STORAGE_VERSION = 1;
export const RESULTS_STORAGE_PREFIX = 'load-balancer:results:';
export const RESULTS_INDEX_KEY = `${RESULTS_STORAGE_PREFIX}index`;
const RECORD_PREFIX = `${RESULTS_STORAGE_PREFIX}record:`;
const DELETED_RUN_PREFIX = `${RESULTS_STORAGE_PREFIX}deleted:`;
export const DELETED_RUN_LIMIT = 256;
const INDEX_BYTE_LIMIT = 1024 * 1024;

export interface ResultsStorage {
  readonly length: number;
  key(index: number): string | null;
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface ReadyResultEntry {
  readonly status: 'ready';
  readonly id: string;
  readonly runId: string;
  readonly resultVersion: number;
  readonly completionReason: RunResult['completion']['reason'];
  readonly receivedAt: number;
  readonly complete: boolean;
  readonly sampleCount: number;
  readonly virtualTimeStartSeconds: number | null;
  readonly virtualTimeEndSeconds: number | null;
  readonly seed: number | string | null;
  readonly model: string | null;
  readonly algorithm: string | null;
}

export interface RecoverableResultEntry {
  readonly status: 'recoverable';
  readonly id: string;
  readonly warning: string;
}

export type ResultIndexEntry = ReadyResultEntry | RecoverableResultEntry;
export interface ResultsListing {
  readonly entries: readonly ResultIndexEntry[];
  readonly warnings: readonly string[];
}

export interface ResultsRepository {
  list(): ResultsListing;
  read(id: string): RunResult | null;
  // New observations must use a fresh UUID, as ResultsSession does. Collision
  // checks protect existing records; synchronous localStorage cannot atomically
  // arbitrate two tabs deliberately creating different payloads under one new ID.
  save(result: RunResult): ReadyResultEntry;
  remove(id: string): void;
  isDeletedRun(runId: string): boolean;
  recover(id: string): RunResult;
}

export type RepositoryOperation = 'list' | 'read' | 'save' | 'remove' | 'recover';
export type RepositoryErrorCode = 'unavailable' | 'quota' | 'corrupt' | 'unsupported-version' | 'too-large' | 'conflict' | 'not-found' | 'invalid';

export class RepositoryError extends Error {
  readonly code: RepositoryErrorCode;
  readonly operation: RepositoryOperation;
  readonly id: string | undefined;
  // True only when saving/recovering left this candidate's full record durable.
  readonly durableRecord: boolean;

  constructor(code: RepositoryErrorCode, operation: RepositoryOperation, message: string, id?: string, durableRecord = false) {
    super(message);
    this.name = 'RepositoryError';
    this.code = code;
    this.operation = operation;
    this.id = id;
    this.durableRecord = durableRecord;
  }
}

export interface RepositoryOptions {
  readonly recordByteLimit?: number;
  readonly indexByteLimit?: number;
}

export function resultStorageKey(id: string): string {
  return `${RECORD_PREFIX}${encodeURIComponent(id)}`;
}

export function deletedRunStorageKey(runId: string): string {
  return `${DELETED_RUN_PREFIX}${encodeURIComponent(runId)}`;
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function nonempty(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0;
}

function nullableNumber(value: unknown): boolean {
  return value === null || typeof value === 'number' && Number.isFinite(value) && value >= 0;
}

function indexEntry(value: unknown): value is ReadyResultEntry {
  return record(value) && value.status === 'ready' && nonempty(value.id) && nonempty(value.runId)
    && Number.isSafeInteger(value.resultVersion) && (value.resultVersion as number) >= 1
    && (value.completionReason === 'finished' || value.completionReason === 'stopped')
    && typeof value.receivedAt === 'number' && Number.isFinite(value.receivedAt)
    && typeof value.complete === 'boolean' && Number.isSafeInteger(value.sampleCount) && (value.sampleCount as number) >= 0
    && nullableNumber(value.virtualTimeStartSeconds) && nullableNumber(value.virtualTimeEndSeconds)
    && (value.seed === null || typeof value.seed === 'string' && /^-?(0|[1-9]\d*)$/.test(value.seed)
      || typeof value.seed === 'number' && Number.isSafeInteger(value.seed))
    && (value.model === null || typeof value.model === 'string')
    && (value.algorithm === null || typeof value.algorithm === 'string');
}

function rowFor(result: RunResult): ReadyResultEntry {
  let start: number | null = null;
  let end: number | null = null;
  let algorithm: string | null = null;
  const sequence = result.summary.observation?.publicationSequence;
  for (const sample of result.collection.samples) {
    start = start === null ? sample.virtualTimeSeconds : Math.min(start, sample.virtualTimeSeconds);
    end = end === null ? sample.virtualTimeSeconds : Math.max(end, sample.virtualTimeSeconds);
    if (sample.publicationSequence === sequence) algorithm = sample.snapshot.algorithm;
  }
  return Object.freeze({
    status: 'ready', id: result.id, runId: result.runId, resultVersion: result.resultVersion,
    completionReason: result.completion.reason, receivedAt: result.completion.receivedAt,
    complete: result.coverage.complete, sampleCount: result.collection.samples.length,
    virtualTimeStartSeconds: start, virtualTimeEndSeconds: end,
    seed: result.context.seed, model: result.context.experiment?.model ?? null, algorithm,
  });
}

function sameJSON(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length
      && left.every((value, index) => sameJSON(value, right[index]));
  }
  if (!record(left) || !record(right)) return false;
  const keys = Object.keys(left);
  return keys.length === Object.keys(right).length
    && keys.every((key) => Object.prototype.hasOwnProperty.call(right, key) && sameJSON(left[key], right[key]));
}

function errorFor(error: unknown, operation: RepositoryOperation, id?: string, durableRecord = false): RepositoryError {
  if (error instanceof RepositoryError) {
    if (error.operation === operation && (!durableRecord || error.durableRecord)) return error;
    return new RepositoryError(error.code, operation, error.message, id, durableRecord || error.durableRecord);
  }
  if (error instanceof ResultValidationError) {
    return new RepositoryError(error.code === 'invalid' && operation !== 'save' ? 'corrupt' : error.code,
      operation, error.message, id, durableRecord);
  }
  const name = record(error) || error instanceof Error ? error.name : undefined;
  const quota = name === 'QuotaExceededError' || name === 'NS_ERROR_DOM_QUOTA_REACHED';
  const quotaMessage = operation === 'save'
    ? 'Local Results storage is full. The Results candidate remains in memory.'
    : 'Local Results storage is full. This operation could not be completed.';
  return new RepositoryError(quota ? 'quota' : 'unavailable', operation,
    quota ? quotaMessage
      : 'Local Results storage is unavailable. Existing records have not been cleared.', id, durableRecord);
}

interface ReadIndex {
  entries: ReadyResultEntry[];
  warning: string | null;
  unsupported: boolean;
}

// Record keys are durable authority; the small index is a replaceable list cache.
// Every mutation rereads it. localStorage offers no cross-tab compare-and-swap:
// enumeration makes any record hidden by a racing index write recoverable.
export class LocalResultsRepository implements ResultsRepository {
  private readonly source: ResultsStorage | (() => ResultsStorage);
  private readonly recordByteLimit: number;
  private readonly indexByteLimit: number;
  private markerCleanupWarning: string | null = null;

  constructor(storage: ResultsStorage | (() => ResultsStorage) = () => window.localStorage, options: RepositoryOptions = {}) {
    this.source = storage;
    this.recordByteLimit = options.recordByteLimit ?? RESULT_RECORD_BYTE_LIMIT;
    this.indexByteLimit = options.indexByteLimit ?? INDEX_BYTE_LIMIT;
    if (![this.recordByteLimit, this.indexByteLimit].every((limit) => Number.isSafeInteger(limit) && limit > 0)) {
      throw new Error('Repository size limits must be positive safe integers.');
    }
  }

  list(): ResultsListing {
    try {
      const storage = this.storage();
      const index = this.readIndex(storage);
      const ids = this.recordIds(storage);
      const warnings = index.warning === null ? [] : [index.warning];
      if (this.markerCleanupWarning) warnings.push(this.markerCleanupWarning);
      const entries: ResultIndexEntry[] = [];
      for (const row of index.entries) {
        if (ids.delete(row.id)) entries.push(row);
      }
      for (const id of ids) entries.push({
        status: 'recoverable', id,
        warning: 'This record is missing a valid list entry. Open or recover it to validate its contents.',
      });
      entries.sort((a, b) => (b.status === 'ready' ? b.receivedAt : -Infinity)
        - (a.status === 'ready' ? a.receivedAt : -Infinity) || a.id.localeCompare(b.id));
      return freezeTree({ entries, warnings });
    } catch (error) { throw errorFor(error, 'list'); }
  }

  read(id: string): RunResult | null {
    try {
      this.requireId(id);
      return this.readRecord(this.storage(), id);
    } catch (error) { throw errorFor(error, 'read', id); }
  }

  save(result: RunResult): ReadyResultEntry {
    let durable = false;
    let id: string | undefined;
    try {
      validateRunResult(result);
      id = result.id;
      const json = JSON.stringify({ storageVersion: STORAGE_VERSION, result });
      requireJSONSize(json, this.recordByteLimit);
      const storage = this.storage();
      const existing = storage.getItem(resultStorageKey(id));
      if (existing !== null) {
        const previous = this.parseRecord(existing, id);
        if (!sameJSON(previous, result)) throw new RepositoryError('conflict', 'save', 'A different result already uses this local ID. The saved record was preserved.', id);
      } else storage.setItem(resultStorageKey(id), json);
      durable = true;
      const row = rowFor(result);
      // Persist the full record first. A failing index update must never roll it back.
      this.upsertIndex(storage, row);
      return row;
    } catch (error) { throw errorFor(error, 'save', id, durable); }
  }

  remove(id: string): void {
    try {
      this.requireId(id);
      const storage = this.storage();
      const index = this.readIndex(storage);
      let runId = index.entries.find(entry => entry.id === id)?.runId ?? null;
      try {
        runId = this.readRecord(storage, id)?.runId ?? runId;
      } catch {
        // A damaged record can still be deleted using its validated index identity.
      }
      if (runId !== null) {
        const key = deletedRunStorageKey(runId);
        const existingMarker = storage.getItem(key);
        if (existingMarker !== null) this.deletedAt(existingMarker);
        // Remember the dismissal before deletion; cached end events must not restore it.
        storage.setItem(key, JSON.stringify({
          storageVersion: STORAGE_VERSION, deletedAt: Date.now(),
        }));
      }
      storage.removeItem(resultStorageKey(id));
      if (index.unsupported) throw new RepositoryError('unsupported-version', 'remove', 'The record was removed, but this index version cannot be updated.', id);
      // Dropped/stale rows are harmless: list always intersects with record keys.
      this.writeIndex(storage, index.entries.filter((entry) => entry.id !== id));
      if (runId !== null) this.pruneDeletedRuns(storage, deletedRunStorageKey(runId));
    } catch (error) { throw errorFor(error, 'remove', id); }
  }

  isDeletedRun(runId: string): boolean {
    try {
      this.requireId(runId);
      const json = this.storage().getItem(deletedRunStorageKey(runId));
      if (json === null) return false;
      this.deletedAt(json);
      return true;
    } catch (error) { throw errorFor(error, 'read'); }
  }

  recover(id: string): RunResult {
    let durable = false;
    try {
      this.requireId(id);
      const storage = this.storage();
      const result = this.readRecord(storage, id);
      if (result === null) throw new RepositoryError('not-found', 'recover', 'This local result no longer exists.', id);
      durable = true;
      this.upsertIndex(storage, rowFor(result));
      return result;
    } catch (error) { throw errorFor(error, 'recover', id, durable); }
  }

  private storage(): ResultsStorage {
    const storage = typeof this.source === 'function' ? this.source() : this.source;
    if (!storage) throw new Error('Storage is unavailable.');
    return storage;
  }

  private deletedAt(json: string): number {
    requireJSONSize(json, 4096);
    let marker: unknown;
    try { marker = JSON.parse(json); } catch {
      throw new ResultValidationError('invalid', 'A deleted-run marker contains invalid JSON.');
    }
    if (!record(marker)) throw new ResultValidationError('invalid', 'A deleted-run marker is invalid.');
    if (marker.storageVersion !== STORAGE_VERSION) {
      throw new ResultValidationError('unsupported-version', 'This deleted-run marker version is not supported.');
    }
    if (typeof marker.deletedAt !== 'number' || !Number.isFinite(marker.deletedAt)) {
      throw new ResultValidationError('invalid', 'A deleted-run marker timestamp is invalid.');
    }
    return marker.deletedAt;
  }

  private pruneDeletedRuns(storage: ResultsStorage, newestKey: string) {
    try {
      const older: { key: string; deletedAt: number }[] = [];
      for (let index = 0; index < storage.length; index++) {
        const key = storage.key(index);
        if (key === null || key === newestKey || !key.startsWith(DELETED_RUN_PREFIX)) continue;
        const json = storage.getItem(key);
        if (json === null) continue;
        try { older.push({ key, deletedAt: this.deletedAt(json) }); } catch {
          // Preserve unknown or damaged markers instead of weakening their suppression.
        }
      }
      older.sort((left, right) => right.deletedAt - left.deletedAt || left.key.localeCompare(right.key));
      for (const marker of older.slice(DELETED_RUN_LIMIT - 1)) storage.removeItem(marker.key);
      this.markerCleanupWarning = null;
    } catch {
      // Extra markers are safer than undoing a successful deletion when cleanup fails.
      this.markerCleanupWarning = 'The result was deleted, but old deletion markers could not be cleaned up.';
    }
  }

  private requireId(id: string): void {
    if (!nonempty(id)) throw new ResultValidationError('invalid', 'A local result ID is required.');
  }

  private recordIds(storage: ResultsStorage): Set<string> {
    const ids = new Set<string>();
    for (let index = 0; index < storage.length; index++) {
      const key = storage.key(index);
      if (key === null || !key.startsWith(RECORD_PREFIX)) continue;
      const suffix = key.slice(RECORD_PREFIX.length);
      try {
        const id = decodeURIComponent(suffix);
        if (nonempty(id) && resultStorageKey(id) === key) ids.add(id);
      } catch { /* A malformed foreign suffix is not an owned result key. */ }
    }
    return ids;
  }

  private readRecord(storage: ResultsStorage, id: string): RunResult | null {
    const json = storage.getItem(resultStorageKey(id));
    return json === null ? null : this.parseRecord(json, id);
  }

  private parseRecord(json: string, id: string): RunResult {
    requireJSONSize(json, this.recordByteLimit);
    let value: unknown;
    try { value = JSON.parse(json); } catch { throw new ResultValidationError('invalid', 'This archived result contains invalid JSON.'); }
    if (!record(value)) throw new ResultValidationError('invalid', 'This archived result has an invalid envelope.');
    if (value.storageVersion !== STORAGE_VERSION) throw new ResultValidationError('unsupported-version', 'This result storage version is not supported.');
    validateRunResult(value.result, id);
    return freezeTree(value.result);
  }

  private readIndex(storage: ResultsStorage): ReadIndex {
    const json = storage.getItem(RESULTS_INDEX_KEY);
    if (json === null) return { entries: [], warning: null, unsupported: false };
    try {
      requireJSONSize(json, this.indexByteLimit);
      const value: unknown = JSON.parse(json);
      if (!record(value)) throw new Error('Invalid index.');
      if (value.storageVersion !== STORAGE_VERSION) return {
        entries: [], warning: 'The local Results index uses an unsupported storage version. Existing record keys remain available.', unsupported: true,
      };
      if (!Array.isArray(value.entries)) throw new Error('Invalid index.');
      const seen = new Set<string>();
      const entries: ReadyResultEntry[] = [];
      let invalid = false;
      for (const entry of value.entries) {
        if (!indexEntry(entry) || seen.has(entry.id)) { invalid = true; continue; }
        seen.add(entry.id);
        entries.push(entry);
      }
      return { entries, warning: invalid ? 'Some local Results list entries are invalid. Their record keys remain available for recovery.' : null, unsupported: false };
    } catch {
      return { entries: [], warning: 'The local Results index is damaged. Existing record keys remain available for recovery.', unsupported: false };
    }
  }

  private upsertIndex(storage: ResultsStorage, row: ReadyResultEntry): void {
    const index = this.readIndex(storage);
    if (index.unsupported) throw new RepositoryError('unsupported-version', 'save', 'The full result is retained, but this index version cannot be updated.', row.id, true);
    this.writeIndex(storage, [...index.entries.filter((entry) => entry.id !== row.id), row]);
  }

  private writeIndex(storage: ResultsStorage, entries: ReadyResultEntry[]): void {
    const json = JSON.stringify({ storageVersion: STORAGE_VERSION, entries });
    requireJSONSize(json, this.indexByteLimit);
    storage.setItem(RESULTS_INDEX_KEY, json);
  }
}
