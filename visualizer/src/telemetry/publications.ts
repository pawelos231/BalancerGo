import type { PublicationSequence, SimulationMetadata } from '../types.ts';

export interface PublicationObservation {
  kind: 'idle' | 'baseline' | 'contiguous' | 'gap' | 'duplicate' | 'stale';
  runId: string | null;
  publicationSequence: PublicationSequence | null;
  firstObservedSequence: PublicationSequence | null;
  highestSeenSequence: PublicationSequence | null;
  missingSinceLast: string;
  missingTotal: string;
  newPublication: boolean;
  acceptSnapshot: boolean;
}

interface RunCoverage {
  runId: string;
  firstObservedSequence: PublicationSequence;
  highestSeenSequence: PublicationSequence;
  missingTotal: bigint;
}

// Observes validated wire publications before UI coalescing. Only the current run is retained:
// idle/new runs start fresh, and attaching at N does not imply earlier publications were lost.
export class PublicationTracker {
  private run: RunCoverage | null = null;
  private reconnecting = false;

  // Keep coverage across a socket reconnect, but let its cached frame rehydrate the view once.
  reconnect() {
    this.reconnecting = true;
  }

  observe(simulation: SimulationMetadata): PublicationObservation {
    if (simulation.status === 'idle') {
      this.run = null;
      this.reconnecting = false;
      return {
        kind: 'idle', runId: null, publicationSequence: null,
        firstObservedSequence: null, highestSeenSequence: null,
        missingSinceLast: '0', missingTotal: '0', newPublication: false, acceptSnapshot: true,
      };
    }

    const { runId, publicationSequence } = simulation;
    let kind: PublicationObservation['kind'];
    let missingSinceLast = 0n;
    let newPublication = true;
    let acceptSnapshot = true;
    if (!this.run || this.run.runId !== runId) {
      this.run = {
        runId, firstObservedSequence: publicationSequence,
        highestSeenSequence: publicationSequence, missingTotal: 0n,
      };
      kind = 'baseline';
    } else {
      const incoming = BigInt(publicationSequence);
      const previous = BigInt(this.run.highestSeenSequence);
      if (incoming <= previous) {
        kind = incoming === previous ? 'duplicate' : 'stale';
        newPublication = false;
        acceptSnapshot = kind === 'duplicate' && this.reconnecting;
      } else {
        missingSinceLast = incoming - previous - 1n;
        kind = missingSinceLast > 0n ? 'gap' : 'contiguous';
        this.run.highestSeenSequence = publicationSequence;
        this.run.missingTotal += missingSinceLast;
      }
    }
    if (acceptSnapshot) this.reconnecting = false;
    return {
      kind, runId, publicationSequence,
      firstObservedSequence: this.run.firstObservedSequence,
      highestSeenSequence: this.run.highestSeenSequence,
      missingSinceLast: missingSinceLast.toString(), missingTotal: this.run.missingTotal.toString(),
      newPublication, acceptSnapshot,
    };
  }
}
