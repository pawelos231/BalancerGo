// Categories describe the main variable, rather than promising a measured outcome.
export const presetCategories = [
  { id: 'transport', label: 'TCP transport' },
  { id: 'links', label: 'Links and latency' },
  { id: 'queues', label: 'Queues and shared limits' },
  { id: 'host-cpu', label: 'Host CPU processing' },
  { id: 'loss', label: 'Packet loss' },
  { id: 'workload', label: 'Workload and arrivals' },
  { id: 'routing', label: 'Routing' },
  { id: 'edge-cases', label: 'Edge cases' },
  { id: 'performance', label: 'Simulator performance' },
  { id: 'reference', label: 'Reference experiments' },
  { id: 'timeline', label: 'Timeline changes' },
] as const;

export type ScenarioPresetCategory = typeof presetCategories[number]['id'];

// Source is canonical; loading a preset compiles and validates that text.
export interface ScenarioPreset {
  id: string;
  category: ScenarioPresetCategory;
  title: string;
  description: string;
  source: string;
}
