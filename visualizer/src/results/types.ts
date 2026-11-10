import type { ExactTelemetry } from '../telemetry/exact.ts';
import type { SimulationSnapshot, TopologyDefinition } from '../telemetry/types.ts';
import type { PublicationSequence } from '../types.ts';

// The raw collection is independent of completed results and their storage policy.
export const COLLECTION_VERSION = 1;

export interface CollectedSample {
  readonly index: number;
  readonly publicationSequence: PublicationSequence;
  readonly virtualTimeSeconds: number;
  readonly receivedAt: number;
  readonly snapshot: ExactTelemetry<SimulationSnapshot>;
}

export type ObservationMarker = {
  readonly kind: 'lateJoin';
  readonly publicationSequence: PublicationSequence;
  readonly receivedAt: number;
} | {
  readonly kind: 'gap';
  readonly afterSequence: PublicationSequence;
  readonly beforeSequence: PublicationSequence;
  readonly receivedAt: number;
} | {
  readonly kind: 'disconnected' | 'reconnected';
  readonly receivedAt: number;
};

export interface CollectionLimits {
  readonly memoryLimited: boolean;
  // Rejected deliveries, rather than unique IDs: dropped IDs are not retained.
  readonly droppedSamples: number;
  readonly droppedDefinitions: number;
  readonly droppedMarkers: number;
  readonly markerLimitReached: boolean;
  readonly terminalSnapshotDropped: boolean;
}

export interface CollectedRun {
  readonly collectionVersion: typeof COLLECTION_VERSION;
  readonly runId: string;
  readonly createdAt: number;
  readonly samples: readonly CollectedSample[];
  readonly definitions: readonly ExactTelemetry<TopologyDefinition>[];
  readonly markers: readonly ObservationMarker[];
  readonly firstObservedSequence: PublicationSequence | null;
  readonly highestObservedSequence: PublicationSequence | null;
  readonly terminalPublicationSequence: PublicationSequence | null;
  readonly limits: CollectionLimits;
}

export interface CollectionUsage {
  // Conservative retained serialized UTF-8 bound, not a JavaScript heap estimate.
  readonly serializedBytes: number;
  readonly byteBudget: number;
  readonly terminalReserveBytes: number;
  readonly runCount: number;
  readonly memoryLimited: boolean;
  readonly runLimitReached: boolean;
  readonly droppedRunRecords: number;
}
