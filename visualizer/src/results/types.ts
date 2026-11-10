import type { ExactTelemetry } from '../telemetry/exact.ts';
import type { ExperimentContext } from '../experiment.ts';
import type { Scenario } from '../scenarios/types.ts';
import type { NodeTelemetry, ServerTelemetry, SimulationSnapshot, TopologyDefinition } from '../telemetry/types.ts';
import type { TopologyLinkStats } from '../topology/types.ts';
import type {
  PublicationSequence, SimulationPayloadProgress, SimulationPerformanceMetrics,
  SimulationTransferResults,
} from '../types.ts';

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
  readonly droppedMetadata: number;
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
  readonly firstObservedAt: number | null;
  readonly highestObservedSequence: PublicationSequence | null;
  readonly terminalPublicationSequence: PublicationSequence | null;
  readonly metadata: CollectedMetadata | null;
  readonly limits: CollectionLimits;
}

// A submitted document is tied to its accepted run ID, never to the current draft.
export interface CollectedMetadata {
  readonly provenance: 'backend' | 'submitted';
  readonly receivedAt: number;
  readonly scenario: ExactTelemetry<Scenario>;
  readonly experiment: ExactTelemetry<ExperimentContext> | null;
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

export const RUN_RESULT_VERSION = 1;

export interface SampleReference {
  readonly index: number;
  readonly publicationSequence: PublicationSequence;
  readonly virtualTimeSeconds: number;
  readonly receivedAt: number;
  readonly topologyRevision: string;
}

export interface LastObservation<T> {
  readonly id: string;
  readonly observation: SampleReference;
  // Absence describes the last summary sample; it does not erase the last value.
  readonly presentInSummarySample: boolean;
  readonly value: ExactTelemetry<T>;
}

export interface HostCPUSummary {
  readonly basis: 'hostLifetimeMean';
  readonly utilizationRatio: number | string | null;
  readonly busyTimeSeconds: number | string | null;
  readonly availableCoreTimeSeconds: number | string | null;
}

export interface NodeResultSummary extends LastObservation<NodeTelemetry> {
  readonly kind: string | null;
  readonly groupId: string | null;
  readonly cpu: HostCPUSummary;
}

export interface ServerResultSummary extends LastObservation<ServerTelemetry> {
  readonly cpu: HostCPUSummary;
}

export interface RunSummary {
  readonly basis: 'terminal' | 'lastObservation' | 'unavailable';
  readonly observation: SampleReference | null;
  // These are cumulative values from one publication, never sums of frames.
  readonly payload: ExactTelemetry<SimulationPayloadProgress> | null;
  readonly transfers: ExactTelemetry<SimulationTransferResults> | null;
  readonly performance: ExactTelemetry<SimulationPerformanceMetrics> | null;
  readonly payloadRate: {
    readonly basis: 'runVirtualTime';
    readonly virtualTimeSeconds: number | string;
    // Derived display rates are approximate; the exact byte counters remain above.
    readonly approximateReceivedBytesPerSecond: number | null;
    readonly approximateAcknowledgedBytesPerSecond: number | null;
  } | null;
  readonly transferLatencyP95Seconds: null;
  readonly transferLatencyP99Seconds: null;
  readonly resources: {
    readonly nodes: readonly NodeResultSummary[];
    readonly servers: readonly ServerResultSummary[];
    readonly clients: readonly LastObservation<ServerTelemetry['clients'][number] & { serverId: string }>[];
    readonly links: readonly LastObservation<{ id: string; stats: TopologyLinkStats }>[];
    readonly ingress: LastObservation<TopologyLinkStats> | null;
    readonly queues: readonly LastObservation<{ id: string; stats: TopologyLinkStats }>[];
  };
}

export interface PublicationRange {
  readonly first: PublicationSequence;
  readonly last: PublicationSequence;
}

export interface ResultCoverage {
  readonly complete: boolean;
  readonly joinedAfterStart: boolean;
  // Historical attachment boundary, even if an earlier publication arrives later.
  readonly publicationsBeforeFirstReceipt: PublicationRange | null;
  // Final gaps in retained data, including memory rejections and a missing prefix.
  readonly unrecordedPublications: readonly PublicationRange[];
  readonly unrecordedPublicationCount: string;
  readonly disconnections: number;
  readonly missingDefinitionRevisions: readonly string[];
  readonly missingScenario: boolean;
  readonly missingAuthoritativeScenario: boolean;
  readonly missingExperiment: boolean;
  readonly metadataAfterFirstSample: boolean;
  readonly terminalSampleMissing: boolean;
  readonly memoryLimited: boolean;
  readonly limits: CollectionLimits;
}

export interface RunResult {
  readonly resultVersion: typeof RUN_RESULT_VERSION;
  readonly id: string;
  readonly runId: string;
  readonly completion: {
    readonly reason: 'finished' | 'stopped';
    readonly receivedAt: number;
  };
  readonly context: {
    readonly scenario: ExactTelemetry<Scenario> | null;
    readonly scenarioProvenance: 'backend' | 'submitted' | 'missing';
    readonly scenarioReceivedAt: number | null;
    readonly experiment: ExactTelemetry<ExperimentContext> | null;
    readonly experimentProvenance: 'backend' | 'snapshot' | 'missing';
    readonly experimentReceivedAt: number | null;
    readonly seed: number | string | null;
    readonly publicationInterval: {
      readonly clock: 'real';
      readonly configuredNanoseconds: number | string | null;
    };
  };
  readonly summary: RunSummary;
  readonly coverage: ResultCoverage;
  readonly collection: CollectedRun;
}
