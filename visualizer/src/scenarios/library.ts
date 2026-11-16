import { presetCategories, scenarioPresets } from './presets.ts';
import { sourceExamples } from './sourceExamples.ts';
import type { ScenarioSourceExample } from './sourceExamples.ts';

const libraryEntries = [
  ...scenarioPresets.map(preset => ({ ...preset, kind: 'preset' as const })),
  ...sourceExamples,
];
type LibraryEntry = typeof libraryEntries[number];

// Expected failures belong to an explicit filter, away from ordinary scenarios.
function entryCategory(entry: LibraryEntry): string {
  if (entry.kind === 'sourceExample' && entry.expectedDiagnostic) return 'diagnostics';
  return entry.category;
}

export const libraryCategories = [
  { id: 'all', label: 'Scenarios and definitions' },
  ...presetCategories,
  { id: 'distributions', label: 'Distribution definitions' },
  { id: 'diagnostics', label: 'Diagnostics and unsupported drafts' },
].map(category => ({
  ...category,
  count: filterLibraryEntries(category.id, '').length,
}));

// Searching the default list never quietly includes deliberately rejected sources.
export function filterLibraryEntries(category: string, search: string): LibraryEntry[] {
  const query = search.trim().toLowerCase();
  return libraryEntries.filter(entry => {
    const actualCategory = entryCategory(entry);
    const matchesCategory = category === 'all'
      ? actualCategory !== 'diagnostics'
      : actualCategory === category;
    const text = `${entry.title} ${entry.description} ${entry.id}`.toLowerCase();
    return matchesCategory && text.includes(query);
  });
}

export function sourceExampleNotice(example: ScenarioSourceExample): string {
  const nextStep = example.expectedDiagnostic
    ? `Expected diagnostic: ${example.expectedDiagnostic} Edit the source or discard it.`
    : 'Compile and apply to use this scenario. Named definitions keep runtime settings scalar.';
  return `Loaded source example: ${example.title}. ${nextStep}`;
}
