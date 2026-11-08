import type { SimulationMetadata, StopCommandResult } from '../types';
import { CommandType } from '../commands.ts';

export interface StopIntent {
  runId: string;
  commandId: string;
}

export function isStopCommandResult(message: unknown): message is StopCommandResult {
  if (message === null || typeof message !== 'object') return false;
  const value = message as Record<string, unknown>;
  return value.type === 'commandResult' && value.commandType === CommandType.StopSimulation
    && typeof value.runId === 'string' && typeof value.commandId === 'string'
    && (value.status === 'applied' || value.status === 'rejected')
    && (value.error === undefined || typeof value.error === 'string');
}

// Keep command completion separate from observations of the shared simulation.
export class StopCommands {
  private intent: StopIntent | null = null;
  private clearedRunIds = new Set<string>();

  get pending(): StopIntent | null {
    return this.intent;
  }

  send(runId: string, commandId: string, send: (intent: StopIntent) => void): boolean {
    if (this.intent || !runId || !commandId) return false;
    const intent = { runId, commandId };
    this.intent = intent;
    try {
      send(intent);
    } catch (error) {
      if (this.intent === intent) this.intent = null;
      throw error;
    }
    return true;
  }

  finish(result: StopCommandResult): boolean {
    if (!this.intent || this.intent.runId !== result.runId
      || this.intent.commandId !== result.commandId) return false;
    this.intent = null;
    if (result.status === 'applied') this.clearedRunIds.add(result.runId);
    return true;
  }

  acceptsSnapshot(simulation: Pick<SimulationMetadata, 'status' | 'runId'>): boolean {
    return simulation.status === 'idle'
      || !simulation.runId || !this.hasClearedRun(simulation.runId);
  }

  hasClearedRun(runId: string): boolean {
    return this.clearedRunIds.has(runId);
  }

  disconnect(): boolean {
    const uncertain = this.intent !== null;
    this.intent = null;
    this.clearedRunIds.clear();
    return uncertain;
  }
}
