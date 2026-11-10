import type { ResultIndexEntry, ResultsRepository } from './repository.ts';
import type { ResultsSession } from './session.ts';
import type { RunResult } from './types.ts';

export interface ResultsHistoryState {
  readonly entries: readonly ResultIndexEntry[];
  readonly candidates: readonly RunResult[];
  readonly preparing: readonly string[];
  readonly warning: string | null;
  readonly storageError: string | null;
  readonly storageWarnings: readonly string[];
  readonly saveErrors: Readonly<Record<string, string>>;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : 'The result history could not be accessed.';
}

// A cached end is redundant only if its measurements and context were already retained.
function retainsTerminalEvidence(stored: RunResult, candidate: RunResult): boolean {
  const incoming = candidate.collection.samples[0];
  const previous = stored.collection.samples.find(sample => (
    sample.publicationSequence === incoming.publicationSequence
  ));
  const equal = (left: unknown, right: unknown) => JSON.stringify(left) === JSON.stringify(right);
  if (!previous || !equal(previous.snapshot, incoming.snapshot)) return false;

  const definitions = new Map(stored.collection.definitions.map(definition => [definition.revision, definition]));
  const definitionsKnown = candidate.collection.definitions.every(definition => (
    equal(definitions.get(definition.revision), definition)
  ));
  const scenarioKnown = candidate.context.scenario === null
    || equal(stored.context.scenario, candidate.context.scenario);
  const experimentKnown = candidate.context.experiment === null
    || equal(stored.context.experiment, candidate.context.experiment);
  const provenanceKnown = (candidate.context.scenarioProvenance !== 'backend'
    || stored.context.scenarioProvenance === 'backend')
    && (candidate.context.experimentProvenance !== 'backend'
      || stored.context.experimentProvenance === 'backend');
  return definitionsKnown && scenarioKnown && experimentKnown && provenanceKnown;
}

// Connect completed candidates to storage without tying collection to a page.
// A failed save is attempted again only when the user chooses Retry.
export class ResultsHistory {
  private readonly session: ResultsSession;
  private readonly repository: ResultsRepository;
  private readonly listeners = new Set<() => void>();
  private readonly attempted = new Set<string>();
  private readonly saveErrors = new Map<string, string>();
  private entries: readonly ResultIndexEntry[] = [];
  private storageWarnings: readonly string[] = [];
  private storageError: string | null = null;
  private unsubscribe: (() => void) | null = null;
  private synchronizing = false;
  private cachedState: ResultsHistoryState;

  constructor(session: ResultsSession, repository: ResultsRepository) {
    this.session = session;
    this.repository = repository;
    this.cachedState = this.snapshot();
  }

  readonly getSnapshot = (): ResultsHistoryState => this.cachedState;

  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  start() {
    if (this.unsubscribe) return;
    this.unsubscribe = this.session.subscribe(() => this.synchronize());
    this.synchronize();
  }

  stop() {
    this.unsubscribe?.();
    this.unsubscribe = null;
  }

  readonly refresh = () => {
    try {
      this.readIndex();
      this.storageError = null;
    } catch (error) {
      this.storageError = message(error);
    }
    this.publish();
  };

  readonly read = (id: string): RunResult | null => this.repository.read(id);

  readonly retry = (id: string) => {
    this.attempted.delete(id);
    this.synchronize();
  };

  readonly discardCandidate = (id: string) => {
    this.session.release(id);
  };

  readonly remove = (id: string): boolean => {
    try {
      this.repository.remove(id);
      this.session.release(id);
      this.refresh();
      return true;
    } catch (error) {
      this.storageError = message(error);
      this.refreshKeepingError();
      return false;
    }
  };

  readonly recover = (id: string): RunResult | null => {
    try {
      const result = this.repository.recover(id);
      this.refresh();
      return result;
    } catch (error) {
      this.storageError = message(error);
      this.refreshKeepingError();
      return null;
    }
  };

  private synchronize() {
    // Successful saves release candidates and notify this listener synchronously.
    if (this.synchronizing) return;
    this.synchronizing = true;
    try {
      for (const candidate of this.session.state.candidates) {
        if (this.attempted.has(candidate.id)) continue;
        this.attempted.add(candidate.id);
        try {
          if (!this.isStoredTerminalReplay(candidate)) this.repository.save(candidate);
          this.saveErrors.delete(candidate.id);
          this.session.release(candidate.id);
        } catch (error) {
          this.saveErrors.set(candidate.id, message(error));
        }
      }

      // Only unsuccessful candidates need retry bookkeeping across updates.
      const remaining = new Set(this.session.state.candidates.map(candidate => candidate.id));
      for (const id of this.attempted) {
        if (!remaining.has(id)) {
          this.attempted.delete(id);
          this.saveErrors.delete(id);
        }
      }
      this.refresh();
    } finally {
      this.synchronizing = false;
    }
  }

  private refreshKeepingError() {
    try {
      this.readIndex();
    } catch {
      // Keep the original save/recovery error while retaining the previous list.
    }
    this.publish();
  }

  // Reopening the app can replay the cached end of an already stored run.
  // Keep distinct observations with earlier samples; skip a terminal-only replay.
  private isStoredTerminalReplay(candidate: RunResult): boolean {
    const samples = candidate.collection.samples;
    if (candidate.completion.reason !== 'finished' || samples.length !== 1
      || samples[0].publicationSequence !== candidate.collection.terminalPublicationSequence) return false;
    if (this.repository.isDeletedRun(candidate.runId)) return true;

    const matching = this.repository.list().entries.filter(entry => (
      entry.status === 'ready' && entry.runId === candidate.runId
    ));
    for (const entry of matching) {
      try {
        const stored = this.repository.read(entry.id);
        if (stored?.runId === candidate.runId && retainsTerminalEvidence(stored, candidate)) return true;
      } catch {
        // An unreadable older record must not consume a new valid candidate.
      }
    }
    return false;
  }

  private readIndex() {
    const list = this.repository.list();
    this.entries = Object.freeze([...list.entries]);
    this.storageWarnings = Object.freeze([...list.warnings]);
  }

  private snapshot(): ResultsHistoryState {
    const session = this.session.state;
    const errors = Object.assign(Object.create(null), Object.fromEntries(this.saveErrors));
    return Object.freeze({
      entries: this.entries, candidates: session.candidates, preparing: session.preparing,
      warning: session.warning, storageError: this.storageError,
      storageWarnings: this.storageWarnings, saveErrors: Object.freeze(errors),
    });
  }

  private publish() {
    this.cachedState = this.snapshot();
    for (const listener of this.listeners) listener();
  }
}
