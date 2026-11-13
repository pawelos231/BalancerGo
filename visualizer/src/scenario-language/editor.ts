// A small editable example, independent of whichever JSON preset is currently applied.
export const initialLanguageSource = `run 180s seed 1 speed 1
defaults link 1GB travel 1ms queue 1024
tcp congestion reno mss 1460B

balancer lb {
    ingress 1GB
    egress 1GB
}

backends api {
    count 2
    link 250MB
}

clients regular {
    count 8
    payload 1MB
    every 10ms
    link 10MB
}
`;
import type { ScenarioApi } from '../scenarios/api.ts';
import type { ValidatedScenario } from '../scenarios/types.ts';
import { compileScenario } from './compile.ts';

// Compilation is local; only ready JSON reaches the existing authoritative validation endpoint.
export async function validateLanguageDocument(
  source: string,
  api: Pick<ScenarioApi, 'validate'>,
  signal: AbortSignal,
): Promise<ValidatedScenario> {
  signal.throwIfAborted();
  const scenario = compileScenario(source);
  signal.throwIfAborted();
  const checked = await api.validate(scenario, signal);
  signal.throwIfAborted();
  return checked;
}

