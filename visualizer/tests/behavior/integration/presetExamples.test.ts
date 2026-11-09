import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { parseScenarioV2, validateScenarioV2 } from '../../../src/scenarios/v2.ts';
import type { ScenarioV2 } from '../../../src/scenarios/v2Types.ts';

function readExample(id: string): ScenarioV2 {
  const path = new URL(`../../../../examples/scenarios/${id}.json`, import.meta.url);
  return JSON.parse(readFileSync(path, 'utf8')) as ScenarioV2;
}

// A published snapshot supplies the ordinary defaults used to create these files.
// Go tests separately validate them and compare virtual results after round trips.
test('downloadable examples match the scenarios loaded from the library', () => {
  const defaults = readExample('mixed-clients');

  for (const preset of scenarioPresets) {
    const built = preset.build(defaults);
    const exported = JSON.parse(serializeScenario(built));
    assert.deepEqual(exported, readExample(preset.id), preset.id);
    assert.deepEqual(validateScenarioV2(exported), validateScenarioV2(readExample(preset.id)), preset.id);
  }
});

test('all 37 runnable examples use the strict native timeline contract', () => {
  let count = 0;
  for (const directory of ['scenarios', 'scenarios-v2']) {
    const base = new URL(`../../../../examples/${directory}/`, import.meta.url);
    for (const name of readdirSync(base).filter((entry) => entry.endsWith('.json'))) {
      const document = parseScenarioV2(readFileSync(new URL(name, base), 'utf8'));
      assert.equal(BigInt(document.version), 2n, name);
      assert.deepEqual(Object.keys(document).sort(), ['fixed', 'states', 'version'], name);
      assert.ok(document.states['0'], name);
      count += 1;
    }
  }
  assert.equal(count, 37);
});
