import type { SimulationMetadata } from '../types';
import type { CurrentSimulation } from './types';

// Use only an HTTP read made after acceptance; Stop may erase a run before its frame arrives.
export function httpStartAcknowledged(
  acceptedRunId: string,
  current: Pick<CurrentSimulation, 'runId' | 'status'> | null,
  snapshot: Pick<SimulationMetadata, 'runId' | 'status'> | null,
): boolean {
  if (snapshot?.runId === acceptedRunId) return true;
  if (current === null) return snapshot?.status === 'idle';
  return current.runId !== acceptedRunId;
}
