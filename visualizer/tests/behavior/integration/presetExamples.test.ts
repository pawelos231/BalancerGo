import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import type { Scenario } from '../../../src/scenarios/types.ts';

function readExample(id: string): Scenario {
  const path = new URL(`../../../../examples/scenarios/${id}.json`, import.meta.url);
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
    assert.deepEqual(validateScenario(exported), validateScenario(readExample(preset.id)), preset.id);
  }
});

test('all 40 runnable examples use the strict native timeline contract', () => {
  let count = 0;
  const base = new URL('../../../../examples/scenarios/', import.meta.url);
  for (const name of readdirSync(base).filter((entry) => entry.endsWith('.json'))) {
    const document = parseScenario(readFileSync(new URL(name, base), 'utf8'));
    assert.equal(BigInt(document.scenarioVersion), 2n, name);
    assert.deepEqual(Object.keys(document).sort(), ['fixed', 'scenarioVersion', 'states'], name);
    assert.ok(document.states['0'], name);
    count += 1;
  }
  assert.equal(count, 40);
});
