import { baselinePresets } from './presets/baseline.ts';
import { ecnPresets } from './presets/ecn.ts';
import { linkPresets } from './presets/links.ts';
import { lossPresets } from './presets/loss.ts';
import { performancePresets } from './presets/performance.ts';
import { queuePresets } from './presets/queues.ts';
import { referencePresets } from './presets/reference.ts';
import { routingPresets } from './presets/routing.ts';
import { workloadPresets } from './presets/workload.ts';
import { tcpPresets } from './presets/tcp.ts';
import { hostPresets } from './presets/host.ts';
import { timelinePresets } from './presets/timeline.ts';

export { presetCategories } from './scenarioPresetTypes.ts';
export type { ScenarioPreset } from './scenarioPresetTypes.ts';

// Every builder returns a native scenario document; no format conversion is required.
// Pin the largest benchmark while retaining the original presets' relative order.
export const scenarioPresets = [
  ...performancePresets,
  ...baselinePresets,
  ...linkPresets,
  ...lossPresets,
  ...queuePresets,
  ...ecnPresets,
  ...tcpPresets,
  ...hostPresets,
  ...workloadPresets,
  ...routingPresets,
  ...referencePresets,
  ...timelinePresets,
] as const;
