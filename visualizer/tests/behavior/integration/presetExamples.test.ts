import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { serializeLanguageSource, validateLanguageDocument } from '../../../src/scenario-language/editor.ts';
import { ScenarioApi } from '../../../src/scenarios/api.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { scenarioPresets } from '../../../src/scenarios/presets.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import type { Scenario } from '../../../src/scenarios/types.ts';

function readExample(id: string): Scenario {
  const path = new URL(`../../../../examples/scenarios/${id}.json`, import.meta.url);
  return parseScenario(readFileSync(path, 'utf8'));
}

test('catalog source text is the canonical editable lb file for every preset', () => {
  const directory = new URL('../../../src/scenarios/presets/sources/', import.meta.url);
  assert.deepEqual(
    readdirSync(directory).filter(name => name.endsWith('.lb')).sort(),
    scenarioPresets.map(preset => `${preset.id}.lb`).sort(),
  );

  for (const preset of scenarioPresets) {
    const originalSource = readFileSync(new URL(`${preset.id}.lb`, directory), 'utf8');
    assert.equal(preset.source, originalSource, preset.id);
    assert.equal(serializeLanguageSource(preset.source), originalSource, preset.id);
    assert.match(preset.source, /^language 1$/m, preset.id);
  }
});

// Go tests separately validate the compiled examples and compare resolved configurations.
test('downloadable examples match compiled library source', () => {
  for (const preset of scenarioPresets) {
    const compiled = compileScenario(preset.source);
    const example = readExample(preset.id);
    assert.deepEqual(parseScenario(serializeScenario(compiled)), example, preset.id);
  }
});

test('a source-backed library load validates exact native JSON through the shared HTTP API', async () => {
  const preset = scenarioPresets.find(entry => entry.id === 'simulator-limits')!;
  const sourceBefore = preset.source;
  const compiled = compileScenario(sourceBefore);
  const requests: { url: string; method: string | undefined; body: string }[] = [];
  const api = new ScenarioApi('http://backend.test', async (url, options) => {
    const body = options!.body as string;
    requests.push({ url: String(url), method: options!.method, body });
    return Response.json({
      scenario: parseScenario(body),
      summary: { totalClients: 50_000, backendCount: 100, clientGroupCount: 1 },
    });
  });

  const result = await validateLanguageDocument(sourceBefore, api, new AbortController().signal);
  assert.equal(requests.length, 1);
  assert.equal(requests[0].url, 'http://backend.test/api/scenarios/validate');
  assert.equal(requests[0].method, 'POST');
  assert.deepEqual(JSON.parse(requests[0].body), JSON.parse(serializeScenario(compiled)));
  assert.deepEqual(validateScenario(result.scenario), compiled);
  assert.equal(result.scenario.states['0'].resources.loadBalancer.forwardingBitsPerSecond, '80000000000000000');
  assert.equal(preset.source, sourceBefore);
  assert.equal(serializeLanguageSource(preset.source), sourceBefore);
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
