import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { serializeScenario } from '../src/scenarios/files.ts';
import { scenarioPresets } from '../src/scenarios/presets.ts';
import type { Scenario } from '../src/scenarios/types.ts';

function readExample(id: string): Scenario {
  const path = new URL(`../../examples/scenarios/${id}.json`, import.meta.url);
  return JSON.parse(readFileSync(path, 'utf8')) as Scenario;
}

// A published snapshot supplies the ordinary defaults used to create these files.
// Go tests separately validate them and compare virtual results after round trips.
test('downloadable examples match the scenarios loaded from the library', () => {
  const defaults = readExample('mixed-clients');

  for (const preset of scenarioPresets) {
    const built = preset.build(defaults);
    const exported = JSON.parse(serializeScenario(built));
    assert.deepEqual(exported, readExample(preset.id), preset.id);
  }
});
