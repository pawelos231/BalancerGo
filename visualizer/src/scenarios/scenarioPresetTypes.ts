import type { Scenario } from './types.ts';

// Categories describe the main variable, rather than promising a measured outcome.
export const presetCategories = [
  { id: 'links', label: 'Links and latency' },
  { id: 'queues', label: 'Queues and shared limits' },
  { id: 'loss', label: 'Packet loss' },
  { id: 'workload', label: 'Workload and arrivals' },
  { id: 'routing', label: 'Routing' },
  { id: 'edge-cases', label: 'Edge cases' },
  { id: 'performance', label: 'Simulator performance' },
] as const;

export type ScenarioPresetCategory = typeof presetCategories[number]['id'];

// Each selection builds a fresh native document that the backend can validate.
export interface ScenarioPreset {
  id: string;
  category: ScenarioPresetCategory;
  title: string;
  description: string;
  build: (defaults: Scenario) => Scenario;
}
