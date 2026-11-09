import type { BackendStateV2, V2ClientGroup } from './v2Types.ts';
import { endpointKey } from './v2Values.ts';

const maxClients = 16_777_215n;

interface TimelineIdentity {
  role: 'backend' | 'client group';
  firstPath: string;
  lastState: number;
  endpoint?: string;
  startAt?: bigint;
  clientSlots: bigint;
}

// An ID belongs to one resource for the whole run, including after its removal.
export class TimelineIdentities {
  private readonly entities = new Map<string, TimelineIdentity>();
  private reservedClients = 0n;

  // Retaining backend-1 may change its name or links, but never its endpoint.
  backend(backend: BackendStateV2, path: string, stateIndex: number): void {
    const { identity, firstVisit } = this.visit(backend.id, 'backend', path, stateIndex);
    const endpoint = endpointKey(backend.address, backend.port);

    if (!firstVisit && identity.endpoint !== endpoint) {
      throw new Error(`${path}: retained backend address and port must not change`);
    }
    identity.endpoint = endpoint;
  }

  // A count change from 10 to 1 still reserves slots 0..9 for earlier clients.
  group(group: V2ClientGroup, at: bigint, path: string, stateIndex: number): bigint {
    const { identity, firstVisit } = this.visit(group.id, 'client group', path, stateIndex);
    const startAt = BigInt(group.startAtNanoseconds);

    if (firstVisit) {
      if (startAt < at) {
        throw new Error(`${path}.startAtNanoseconds: new group must start at or after its insertion time`);
      }
      identity.startAt = startAt;
    } else if (identity.startAt !== startAt) {
      throw new Error(`${path}.startAtNanoseconds: retained group start time must not change`);
    }

    const count = BigInt(group.count);
    if (count < 0n) {
      throw new Error(`${path}.count: must be nonnegative`);
    }

    // Smaller budgets and removed groups do not release reserved addresses.
    const additionalSlots = count > identity.clientSlots ? count - identity.clientSlots : 0n;
    if (additionalSlots > maxClients - this.reservedClients) {
      throw new Error(`${path}.count: total reserved clients must not exceed ${maxClients}`);
    }

    this.reservedClients += additionalSlots;
    identity.clientSlots += additionalSlots;
    return identity.clientSlots;
  }

  // Consecutive states may retain an ID; skipping a state means it was removed.
  private visit(
    id: string,
    role: TimelineIdentity['role'],
    path: string,
    stateIndex: number,
  ): { identity: TimelineIdentity; firstVisit: boolean } {
    const previous = this.entities.get(id);
    if (!previous) {
      const identity: TimelineIdentity = {
        role,
        firstPath: path,
        lastState: stateIndex,
        clientSlots: 0n,
      };
      this.entities.set(id, identity);
      return { identity, firstVisit: true };
    }

    if (previous.role !== role) {
      throw new Error(`${path}.id: id ${JSON.stringify(id)} already belongs to ${previous.firstPath}`);
    }
    if (previous.lastState !== stateIndex - 1) {
      throw new Error(`${path}.id: id ${JSON.stringify(id)} cannot be reused after removal`);
    }

    previous.lastState = stateIndex;
    return { identity: previous, firstVisit: false };
  }
}

// ASCII IDs keep generated addresses, references and topology labels unambiguous.
export function validateID(id: string, path: string): void {
  if (id.length < 1 || id.length > 128) {
    throw new Error(`${path}: id length must be in 1..128 bytes`);
  }
  if (/[^a-zA-Z0-9._-]/.test(id)) {
    throw new Error(`${path}: id must contain only ASCII letters, digits, '-', '_' or '.'`);
  }
}

// One state cannot use the same ID for two backends, groups or the LB.
export function registerID(used: Map<string, string>, id: string, path: string): void {
  validateID(id, path);

  const previous = used.get(id);
  if (previous !== undefined) {
    throw new Error(`${path}: id ${JSON.stringify(id)} already used by ${previous}`);
  }
  used.set(id, path);
}
