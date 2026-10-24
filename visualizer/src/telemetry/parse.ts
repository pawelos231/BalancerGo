import type { SimulationSnapshot } from './types.ts';

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

// Validate the envelope before enabling controls; individual resources are checked at hydration.
export function parseSnapshot(value: unknown): SimulationSnapshot {
  if (!record(value) || value.type !== 'simulationSnapshot' || value.version !== 1) {
    throw new Error('The server sent an unsupported simulation snapshot.');
  }
  const { simulation, topology } = value;
  if (!record(simulation) || !record(topology)) throw new Error('The live snapshot is incomplete.');
  const statuses = ['idle', 'running', 'finished', 'failed'];
  const validStatus = typeof simulation.status === 'string' && statuses.includes(simulation.status);
  const validAlgorithm = simulation.algorithm === 'roundRobin' || simulation.algorithm === 'random';
  const validRevision = typeof topology.revision === 'string' && /^(0|[1-9]\d*)$/.test(topology.revision);
  const validTime = typeof simulation.virtualTimeSeconds === 'number'
    && Number.isFinite(simulation.virtualTimeSeconds) && simulation.virtualTimeSeconds >= 0;
  const hasResources = Array.isArray(value.servers)
    && Array.isArray(topology.nodes) && Array.isArray(topology.links);
  const hasRun = simulation.status === 'idle'
    || (typeof simulation.runId === 'string' && simulation.runId.length > 0);
  if (!validStatus || !validAlgorithm || !validRevision || !validTime || !hasResources || !hasRun) {
    throw new Error('The live snapshot is incomplete.');
  }
  return value as unknown as SimulationSnapshot;
}
