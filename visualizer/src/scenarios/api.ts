import type {
  CurrentSimulation,
  Scenario,
  SimulationReceipt,
  ValidatedScenario,
} from './types.ts';
import type { TopologyDefinition } from '../telemetry/types';
import { isExperimentContext } from '../experiment.ts';
import { validateScenario } from './validation.ts';
import { readTelemetryJSON } from '../telemetry/exact.ts';

// Keep API failures readable while retaining the status for expected cases such as 404.
export class ScenarioApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ScenarioApiError';
    this.status = status;
  }
}

// An explicit HTTP URL wins; otherwise use the same backend as the WebSocket.
export function resolveApiBase(apiUrl: string | undefined, websocketUrl: string): string {
  const url = new URL(apiUrl || websocketUrl);
  if (!apiUrl) {
    url.protocol = url.protocol === 'wss:' ? 'https:' : 'http:';
    url.pathname = url.pathname.replace(/\/ws\/?$/, '');
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('The scenario API URL must use HTTP or HTTPS.');
  }
  url.search = '';
  url.hash = '';
  return url.toString().replace(/\/$/, '');
}

// Send documents directly: JSON.stringify preserves exact integer strings unchanged.
export class ScenarioApi {
  private readonly baseUrl: string;
  private readonly fetcher: typeof fetch;

  constructor(baseUrl: string, fetcher: typeof fetch = fetch) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.fetcher = fetcher;
  }

  async defaults(signal?: AbortSignal): Promise<Scenario> {
    const result = await this.request<unknown>('/api/scenarios/defaults', 'GET', undefined, signal);
    if (!isScenario(result)) throw invalidResponse();
    return result;
  }

  async validate(scenario: Scenario, signal?: AbortSignal): Promise<ValidatedScenario> {
    return this.validateDocument(JSON.stringify(scenario), signal);
  }

  // Send file text unchanged so Go can reject duplicate keys and unsafe number tokens.
  async validateDocument(document: string, signal?: AbortSignal): Promise<ValidatedScenario> {
    const result = await this.request<unknown>('/api/scenarios/validate', 'POST', document, signal);
    const summary = isRecord(result) ? result.summary : undefined;
    const summaryKeys = ['totalClients', 'backendCount', 'clientGroupCount'];
    const validSummary = isRecord(summary) && summaryKeys.every((key) => isExactInteger(summary[key]));
    if (!isRecord(result) || !isScenario(result.scenario) || !validSummary) throw invalidResponse();
    if (result.resolved !== undefined && !isResolvedScenario(result.resolved)) throw invalidResponse();
    return result as unknown as ValidatedScenario;
  }

  async start(scenario: Scenario, signal?: AbortSignal): Promise<SimulationReceipt> {
    const result = await this.request<unknown>('/api/simulations', 'POST', scenario, signal);
    if (!isRecord(result) || !isRunId(result.runId) || result.status !== 'accepted') {
      throw invalidResponse();
    }
    return { runId: result.runId, status: 'accepted' };
  }

  async current(signal?: AbortSignal): Promise<CurrentSimulation | null> {
    try {
      const result = await this.request<unknown>('/api/simulations/current', 'GET', undefined, signal);
      const statuses = ['accepted', 'running', 'finished', 'failed'];
      const validStatus = isRecord(result) && typeof result.status === 'string'
        && statuses.includes(result.status);
      if (!isRecord(result) || !isRunId(result.runId) || !validStatus
        || !isScenario(result.scenario) || !isExperimentContext(result.experiment)) {
        throw invalidResponse();
      }
      return result as unknown as CurrentSimulation;
    } catch (error) {
      if (error instanceof ScenarioApiError && error.status === 404) return null;
      throw error;
    }
  }

  async topology(signal?: AbortSignal): Promise<TopologyDefinition> {
    const result = await this.request<unknown>('/api/simulations/current/topology', 'GET', undefined, signal);
    const valid = isRecord(result) && isRunId(result.runId)
      && typeof result.revision === 'string' && /^[1-9]\d*$/.test(result.revision)
      && Array.isArray(result.nodes) && Array.isArray(result.links) && Array.isArray(result.servers);
    if (!valid) throw invalidResponse();
    return result as unknown as TopologyDefinition;
  }

  private async request<T>(
    path: string,
    method: 'GET' | 'POST',
    scenario?: Scenario | string,
    signal?: AbortSignal,
  ): Promise<T> {
    const controller = new AbortController();
    const abort = () => controller.abort(signal?.reason);
    signal?.addEventListener('abort', abort, { once: true });
    if (signal?.aborted) abort();
    const timeout = setTimeout(() => controller.abort(), 15_000);

    try {
      // Browser fetch rejects an unrelated receiver such as this API instance.
      const fetcher = this.fetcher;
      const response = await fetcher(`${this.baseUrl}${path}`, {
        method,
        headers: scenario !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: typeof scenario === 'string' ? scenario : scenario && JSON.stringify(scenario),
        signal: controller.signal,
      });
      const body = await response.text()
        .then(readTelemetryJSON).catch(() => null) as T | { error?: string } | null;
      // A transport may finish after cancellation, including during body decoding.
      controller.signal.throwIfAborted();

      if (!response.ok) {
        const message = body && typeof body === 'object' && 'error' in body
          && typeof body.error === 'string'
          ? body.error
          : `The server rejected the request (HTTP ${response.status}).`;
        throw new ScenarioApiError(response.status, message);
      }
      if (body === null) {
        throw new Error('The scenario API returned an invalid JSON response.');
      }
      return body as T;
    } catch (error) {
      if (signal?.aborted) throw error;
      if (controller.signal.aborted) {
        throw new Error('The scenario API request timed out. Try again.');
      }
      if (error instanceof TypeError) {
        throw new Error('Cannot reach the scenario API. Check the server connection.');
      }
      throw error;
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', abort);
    }
  }
}

// Check response shape here; detailed scenario validation remains authoritative in Go.
function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function isScenario(value: unknown): value is Scenario {
  if (!isRecord(value) || !isRecord(value.fixed) || !isRecord(value.states)) return false;
  const fixed = value.fixed;
  if (!isRecord(fixed.execution) || !isRecord(fixed.tcp)
    || !isRecord(fixed.network) || !isRecord(fixed.network.defaults)
    || !isRecord(fixed.workload) || !isRecord(fixed.workload.defaults)) return false;
  if (!Object.values(value.states).every(state => isRecord(state)
    && isRecord(state.resources) && isRecord(state.resources.loadBalancer)
    && Array.isArray(state.resources.backends)
    && isRecord(state.workload) && Array.isArray(state.workload.clientGroups))) return false;
  try { validateScenario(value); return true; } catch { return false; }
}

function isResolvedScenario(value: unknown): boolean {
  return isRecord(value) && !('version' in value) && String(value.scenarioVersion) === '2'
    && isRecord(value.fixed) && isRecord(value.fixed.execution)
    && isRecord(value.fixed.networkDefaults) && isRecord(value.fixed.defaultStream)
    && Array.isArray(value.states) && value.states.every(state => isRecord(state)
      && isExactInteger(state.atNanoseconds) && isRecord(state.loadBalancer)
      && Array.isArray(state.backends) && Array.isArray(state.clientGroups)
      && isExactInteger(state.totalClients))
    && isExactInteger(value.totalClients);
}

function isExactInteger(value: unknown): boolean {
  return typeof value === 'number'
    ? Number.isSafeInteger(value)
    : typeof value === 'string' && /^-?\d+$/.test(value);
}

function isRunId(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0;
}

function invalidResponse(): Error {
  return new Error('The scenario API returned an unexpected response.');
}
