// Identifies the model, scenario and executable used by one run.
export interface ExperimentContext {
  model: string;
  scenarioSha256: string;
  execution: {
    headless: boolean;
    speed: number;
    traceEnabled: boolean;
  };
  build: {
    goVersion: string;
    moduleVersion: string;
    goos: string;
    goarch: string;
    vcsRevision?: string;
    vcsModified?: boolean;
  };
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function nonempty(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0;
}

// HTTP and WebSocket must accept the same experiment context.
export function isExperimentContext(value: unknown): value is ExperimentContext {
  if (!record(value) || !nonempty(value.model)
    || typeof value.scenarioSha256 !== 'string' || !/^[0-9a-f]{64}$/.test(value.scenarioSha256)) {
    return false;
  }
  const { execution, build } = value;
  return record(execution)
    && typeof execution.headless === 'boolean'
    && typeof execution.speed === 'number' && Number.isFinite(execution.speed) && execution.speed >= 0
    && typeof execution.traceEnabled === 'boolean'
    && record(build)
    && nonempty(build.goVersion) && typeof build.moduleVersion === 'string'
    && nonempty(build.goos) && nonempty(build.goarch)
    && (build.vcsRevision === undefined || typeof build.vcsRevision === 'string')
    && (build.vcsModified === undefined || typeof build.vcsModified === 'boolean');
}
