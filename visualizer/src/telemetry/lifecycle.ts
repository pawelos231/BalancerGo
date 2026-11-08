import { PublicationTracker } from './publications.ts';
import type { PublicationObservation } from './publications.ts';
import type { FinalSample, ParsedSimulationEnded } from './lifecycleParse.ts';
import type { SimulationEnded } from './types.ts';
import type { SimulationMetadata } from '../types.ts';

export interface FinalSampleNotification extends FinalSample {
  source: 'simulationEnded' | 'stopAcknowledgement';
  publicationObservation: PublicationObservation;
}

export interface RunEndedNotification {
  event: SimulationEnded;
  sample: FinalSample | null;
  publicationObservation: PublicationObservation | null;
}

interface RunPublications {
  runId: string;
  tracker: PublicationTracker;
}

export interface EndReceipt extends RunEndedNotification {
  newEnd: boolean;
  display: boolean;
}

// Synchronous consumers see each end before React batches a following run's state.
// Keep only the live and immediately previous publication coverage, plus the latest end ID.
export class RunLifecycleReceiver {
  private current: SimulationMetadata | null = null;
  private publications: RunPublications | null = null;
  private previousPublications: RunPublications | null = null;
  private latestEndRunId: string | null = null;
  private displayedEndRunId: string | null = null;
  private uncertainRunId: string | null = null;
  private readonly ended: (notification: RunEndedNotification) => void;
  private readonly sampled: (notification: FinalSampleNotification) => void;
  private readonly canDisplayRun: (runId: string) => boolean;

  constructor(
    ended: (notification: RunEndedNotification) => void,
    sampled: (notification: FinalSampleNotification) => void = () => {},
    canDisplayRun: (runId: string) => boolean = () => true,
  ) {
    this.ended = ended;
    this.sampled = sampled;
    this.canDisplayRun = canDisplayRun;
  }

  get terminationUncertainty(): string | null {
    return this.uncertainRunId;
  }

  observeSnapshot(simulation: SimulationMetadata): PublicationObservation | null {
    // An ended A frame cannot replace live B or the idle view that cleared A.
    if (simulation.runId === this.latestEndRunId && this.current
      && (this.current.status === 'idle' || this.current.runId !== simulation.runId)) return null;

    let observation: PublicationObservation;
    if (simulation.status === 'idle') {
      if (this.publications) this.previousPublications = this.publications;
      this.publications = null;
      observation = new PublicationTracker().observe(simulation);
    } else {
      observation = this.trackerFor(simulation.runId, true).observe(simulation);
    }
    if (observation.acceptSnapshot) {
      if (simulation.status === 'idle' || simulation.runId !== this.current?.runId) this.displayedEndRunId = null;
      this.current = simulation;
    }
    return observation;
  }

  receiveEnd({ event, sample }: ParsedSimulationEnded): EndReceipt {
    // Reconnecting alone proves nothing; a verified end settles this run's uncertainty.
    if (this.uncertainRunId === event.runId) this.uncertainRunId = null;
    const newEnd = this.latestEndRunId !== event.runId;
    const followsDisplayedEnd = this.current?.runId === this.displayedEndRunId && newEnd
      && this.previousPublications?.runId !== event.runId;
    // A fast run can finish before any running frame reaches an initially idle client.
    const canDisplay = this.canDisplayRun(event.runId) && (!this.current
      || followsDisplayedEnd
      || (this.current.status === 'idle' ? newEnd : this.current.runId === event.runId));
    const observation = sample
      ? this.trackerFor(event.runId, canDisplay).observe(sample.snapshot.simulation)
      : null;
    const display = canDisplay && sample !== null && observation?.kind !== 'stale'
      && (newEnd || observation?.acceptSnapshot === true);
    const receipt = { event, sample, publicationObservation: observation, newEnd, display };

    if (display && sample) {
      this.current = sample.snapshot.simulation;
      this.displayedEndRunId = event.runId;
    }
    if (newEnd) {
      this.latestEndRunId = event.runId;
      this.notifySample(sample, observation, 'simulationEnded');
      this.ended(receipt);
    }
    return receipt;
  }

  receiveStopAcknowledgement(sample: FinalSample | null) {
    if (!sample) return;
    const observation = this.trackerFor(sample.definition.runId, false).observe(sample.snapshot.simulation);
    // Resetting a retained terminal run delivers metrics, without inventing another end.
    this.notifySample(sample, observation, 'stopAcknowledgement');
  }

  disconnect() {
    if (this.current?.status === 'running' && this.latestEndRunId !== this.current.runId) {
      this.uncertainRunId ??= this.current.runId;
    }
    this.reconnect();
  }

  reconnect() {
    this.current = null;
    this.displayedEndRunId = null;
    this.publications?.tracker.reconnect();
    this.previousPublications?.tracker.reconnect();
  }

  private trackerFor(runId: string, makeCurrent: boolean): PublicationTracker {
    if (this.publications?.runId === runId) return this.publications.tracker;
    if (this.previousPublications?.runId === runId) return this.previousPublications.tracker;
    const next = { runId, tracker: new PublicationTracker() };
    if (makeCurrent) {
      if (this.publications) this.previousPublications = this.publications;
      this.publications = next;
    }
    return next.tracker;
  }

  private notifySample(
    sample: FinalSample | null,
    observation: PublicationObservation | null,
    source: FinalSampleNotification['source'],
  ) {
    if (sample && observation?.newPublication) {
      this.sampled({ ...sample, source, publicationObservation: observation });
    }
  }
}
