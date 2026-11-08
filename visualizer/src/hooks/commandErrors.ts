import { CommandType } from '../commands.ts';
import type { CommandError } from '../types.ts';

// Legacy commands and private Start rejections share the same error presentation.
export function readCommandError(message: unknown): Pick<CommandError, 'commandType' | 'error'> | null {
  if (message === null || typeof message !== 'object' || Array.isArray(message)) return null;
  const value = message as Record<string, unknown>;
  if (typeof value.error !== 'string') return null;
  if (value.type === 'commandError' && Object.values(CommandType).includes(value.commandType as CommandType)) {
    return { commandType: value.commandType as CommandType, error: value.error };
  }
  if (value.type === 'commandResult' && value.commandType === CommandType.StartSimulation
    && value.status === 'rejected' && typeof value.runId === 'string' && value.runId.length > 0
    && typeof value.commandId === 'string') {
    return { commandType: CommandType.StartSimulation, error: value.error };
  }
  return null;
}
