import type { StopIntent } from '../hooks/stopCommands.ts';
import type { SimulationEnded } from '../telemetry/types.ts';
import type { StopCommandResult } from '../types.ts';
import { assembleRunResult } from './assembly.ts';
import type { ResultsCollection } from './collection.ts';
import type { RunResult } from './types.ts';

export type StopResultMode = 'clear' | 'save';

export interface ResultsSessionState {
  readonly candidates: readonly RunResult[];
  readonly preparing: readonly string[];
  readonly warning: string | null;
}

interface SessionOptions {
  newId?: () => string;
  prepareMetadata?: (runId: string, signal: AbortSignal) => Promise<void>;
}

interface PendingStop extends StopIntent {
  mode: StopResultMode;
  uncertain: boolean;
  previous: PendingStop | null;
  proof: { status: StopCommandResult['status']; receivedAt: number } | null;
}

interface RunDecision {
  runId: string;
  naturalEnd: RunResult['completion'] | null;
  completion: RunResult['completion'] | null;
  preparation: AbortController | null;
  candidate: RunResult | null;
}

const POLICY_ID_LIMIT = 256;
const CAPACITY_WARNING = 'Results memory is full. Existing data was kept, but this run could not be saved.';
const UNCERTAIN_WARNING = 'Connection lost before Stop was confirmed. Collected data remains in memory; no result was saved for that Stop.';

// Raw publications reach ResultsCollection before these lifecycle methods. This
// layer owns only the local save decision, independently of the live view/reset.
export class ResultsSession {
  private readonly collection: ResultsCollection;
  private readonly options: SessionOptions;
  private readonly runs = new Map<string, RunDecision>();
  // A stop still needs its policy when the raw collection has no free slot.
  // These small identities and the replay guard have explicit fixed bounds.
  private readonly stops = new Map<string, PendingStop>();
  private stopCount = 0;
  private readonly retired = new Set<string>();
  private readonly ineligible = new Set<string>();
  private readonly listeners = new Set<() => void>();
  private observedRunId: string | null = null;
  private failureWarning: string | null = null;
  private disposed = false;
  private cachedState: ResultsSessionState = Object.freeze({
    candidates: Object.freeze([]), preparing: Object.freeze([]), warning: null,
  });

  constructor(collection: ResultsCollection, options: SessionOptions = {}) {
    this.collection = collection;
    this.options = options;
  }

  get state(): ResultsSessionState {
    return this.cachedState;
  }

  acceptsRun(runId: string): boolean {
    return !this.disposed && !this.retired.has(runId);
  }

  subscribe(listener: () => void): () => void {
    if (!this.disposed) this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  }

  requestStop(intent: StopIntent, mode: StopResultMode): boolean {
    if (this.disposed || !intent.runId || !intent.commandId) return false;
    // A natural end accepted before this click already belongs to history.
    if (this.retired.has(intent.runId) || this.runs.get(intent.runId)?.completion) return true;
    const previous = this.stops.get(intent.runId);
    if (previous && (!previous.uncertain || this.matchingStop(intent))) return false;
    if (this.stopCount >= POLICY_ID_LIMIT) {
      this.failureWarning = 'Too many unconfirmed Stops. This Stop cannot create a Results candidate.';
      this.publish();
      return false;
    }
    // A fresh user click may replace an uncertain command after reconnect.
    // Keep its policy for rollback; replacing it is never an automatic retry.
    this.stops.set(intent.runId, { ...intent, mode, uncertain: false, previous: previous ?? null, proof: null });
    this.stopCount++;
    // This provisional allocation cannot use the terminal reserve. Report a
    // failure only if the confirmed save decision also cannot allocate a run.
    this.collection.begin(intent.runId);
    this.publish();
    return true;
  }

  cancelStop(intent: StopIntent): void {
    const pending = this.matchingStop(intent);
    if (!pending || this.disposed) return;
    this.settleStop(pending, 'rejected', Date.now());
  }

  // The live lifecycle tracker calls this only when it adopts a run as current.
  // Keep the latest failed/stopped history available for an explicit save.
  observeRun(runId: string): void {
    if (this.disposed) return;
    this.observedRunId = runId;
    this.releaseOlderIneligible();
  }

  receiveStopResult(result: StopCommandResult, receivedAt = Date.now()): void {
    if (this.disposed) return;
    if (this.retired.has(result.runId)) {
      this.collection.release(result.runId);
      return;
    }
    const pending = this.matchingStop(result);
    if (!pending) return;
    this.settleStop(pending, result.status, receivedAt);
  }

  receiveEnd(event: SimulationEnded, receivedAt = Date.now()): void {
    if (this.disposed) return;
    if (this.retired.has(event.runId)) {
      this.collection.release(event.runId);
      return;
    }
    const pending = this.stops.get(event.runId);
    const matching = event.commandId ? this.matchingStop({ runId: event.runId, commandId: event.commandId }) : undefined;
    if (event.reason === 'stopped' && matching) {
      this.settleStop(matching, 'applied', receivedAt);
      return;
    }
    if (event.reason !== 'finished' || event.finalSnapshot?.simulation.error) {
      if (!this.runs.get(event.runId)?.completion && this.collection.get(event.runId)) {
        this.ineligible.add(event.runId);
        this.releaseOlderIneligible();
      }
      return;
    }
    const run = this.decision(event.runId, receivedAt);
    if (!run || run.completion) return;
    run.naturalEnd ??= { reason: 'finished', receivedAt };
    if (!pending) this.prepare(run, run.naturalEnd);
  }

  disconnected(): void {
    if (this.disposed) return;
    for (const pending of this.stops.values()) pending.uncertain = true;
    this.publish();
  }

  release(id: string): void {
    if (this.disposed) return;
    for (const run of this.runs.values()) {
      if (run.candidate?.id === id) {
        this.discard(run.runId);
        this.publish();
        return;
      }
    }
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    for (const run of this.runs.values()) run.preparation?.abort();
    this.listeners.clear();
    this.runs.clear();
    this.stops.clear();
    this.stopCount = 0;
    this.retired.clear();
    this.ineligible.clear();
  }

  private matchingStop(intent: StopIntent): PendingStop | undefined {
    let pending = this.stops.get(intent.runId);
    while (pending) {
      if (pending.commandId === intent.commandId) return pending;
      pending = pending.previous ?? undefined;
    }
    return undefined;
  }

  private settleStop(pending: PendingStop, status: StopCommandResult['status'], receivedAt: number) {
    // An old acknowledgement proves only its own command. Wait for the newest
    // explicit decision, retaining this proof in case that decision rolls back.
    if (this.stops.get(pending.runId) !== pending) {
      pending.proof ??= { status, receivedAt };
      return;
    }
    if (status === 'applied') {
      this.applied(pending, receivedAt);
      return;
    }
    this.stops.delete(pending.runId);
    this.stopCount--;
    if (pending.previous) {
      const previous = pending.previous;
      this.stops.set(pending.runId, previous);
      if (previous.proof) {
        this.settleStop(previous, previous.proof.status, previous.proof.receivedAt);
        return;
      }
    } else this.reconsider(pending.runId);
    this.releaseOlderIneligible();
    this.publish();
  }

  private forgetStops(runId: string) {
    let pending = this.stops.get(runId);
    while (pending) {
      this.stopCount--;
      pending = pending.previous ?? undefined;
    }
    this.stops.delete(runId);
  }

  private decision(runId: string, receivedAt: number): RunDecision | undefined {
    const existing = this.runs.get(runId);
    if (existing) return existing;
    if (!this.collection.begin(runId, receivedAt, true)) {
      this.failureWarning = CAPACITY_WARNING;
      this.publish();
      return undefined;
    }
    const run: RunDecision = {
      runId, naturalEnd: null, completion: null, preparation: null, candidate: null,
    };
    this.runs.set(runId, run);
    return run;
  }

  private applied(pending: PendingStop, receivedAt: number) {
    this.forgetStops(pending.runId);
    if (pending.mode === 'clear') this.discard(pending.runId);
    else {
      const run = this.decision(pending.runId, receivedAt);
      if (run) this.prepare(run, run.naturalEnd ?? { reason: 'stopped', receivedAt });
      else this.retire(pending.runId);
    }
    this.publish();
  }

  private reconsider(runId: string) {
    const run = this.runs.get(runId);
    if (run?.naturalEnd && !run.completion) this.prepare(run, run.naturalEnd);
  }

  private prepare(run: RunDecision, completion: RunResult['completion']) {
    if (run.completion) return;
    this.ineligible.delete(run.runId);
    run.completion = completion;
    const controller = new AbortController();
    run.preparation = controller;
    this.publish();
    // One bounded adapter operation per eligible end. Failure still assembles
    // the available provenance and coverage instead of losing the measurements.
    void Promise.resolve().then(() => {
      if (!this.disposed && !controller.signal.aborted) {
        return this.options.prepareMetadata?.(run.runId, controller.signal);
      }
    })
      .catch(() => {
        if (!controller.signal.aborted && !this.disposed) {
          this.failureWarning = 'Some run metadata could not be loaded. The Results candidate includes the available data.';
        }
      }).then(() => {
        if (this.disposed || controller.signal.aborted || this.runs.get(run.runId) !== run) return;
        const collected = this.collection.get(run.runId);
        run.preparation = null;
        if (collected) {
          try {
            run.candidate = assembleRunResult(collected, {
              id: this.options.newId?.() ?? crypto.randomUUID(), completion,
            });
          } catch {
            this.failureWarning = 'The Results candidate could not be prepared. Collected data remains in memory.';
          }
        } else this.failureWarning = CAPACITY_WARNING;
        this.publish();
      });
  }

  private discard(runId: string) {
    this.runs.get(runId)?.preparation?.abort();
    this.runs.delete(runId);
    this.forgetStops(runId);
    this.ineligible.delete(runId);
    this.collection.release(runId);
    this.retire(runId);
  }

  private releaseOlderIneligible() {
    if (this.observedRunId === null) return;
    for (const runId of this.ineligible) {
      if (runId !== this.observedRunId && !this.stops.has(runId)
        && !this.runs.get(runId)?.completion) this.discard(runId);
    }
  }

  private retire(runId: string) {
    this.retired.add(runId);
    if (this.retired.size > POLICY_ID_LIMIT) this.retired.delete(this.retired.values().next().value!);
  }

  private publish() {
    const candidates: RunResult[] = [];
    const preparing: string[] = [];
    for (const run of this.runs.values()) {
      if (run.candidate) candidates.push(run.candidate);
      if (run.preparation) preparing.push(run.runId);
    }
    const warning = this.failureWarning
      ?? ([...this.stops.values()].some((pending) => pending.uncertain || pending.previous !== null) ? UNCERTAIN_WARNING : null);
    const previous = this.cachedState;
    if (warning === previous.warning
      && candidates.length === previous.candidates.length
      && candidates.every((candidate, index) => candidate === previous.candidates[index])
      && preparing.length === previous.preparing.length
      && preparing.every((runId, index) => runId === previous.preparing[index])) return;
    this.cachedState = Object.freeze({
      candidates: Object.freeze(candidates), preparing: Object.freeze(preparing), warning,
    });
    for (const listener of this.listeners) listener();
  }
}
