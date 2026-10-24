import { baselinePresets } from './presets/baseline.ts';
import { linkPresets } from './presets/links.ts';
import { lossPresets } from './presets/loss.ts';
import { performancePresets } from './presets/performance.ts';
import { queuePresets } from './presets/queues.ts';
import { routingPresets } from './presets/routing.ts';
import { workloadPresets } from './presets/workload.ts';

export { presetCategories } from './scenarioPresetTypes.ts';
export type { ScenarioPreset } from './scenarioPresetTypes.ts';

// Pin the largest benchmark while retaining the original presets' relative order.
export const scenarioPresets = [
  ...performancePresets,
  ...baselinePresets,
  ...linkPresets,
  ...lossPresets,
  ...queuePresets,
  ...workloadPresets,
  ...routingPresets,
] as const;
