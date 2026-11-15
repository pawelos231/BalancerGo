import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { parseRandomProfile, serializeRandomProfile } from '../../../src/scenarios/randomness.ts';
import { parseScenario } from '../../../src/scenarios/validation.ts';
import { scenarioFixture } from '../../scenarioFixture.ts';

const directory = new URL('../../../../examples/randomness/families/', import.meta.url);
const files = readdirSync(directory).filter(name => name.endsWith('-cases.json'));
assert.ok(files.length > 0, 'Declared distribution families need shared profile cases.');

// Shared raw documents exercise the same exact contract in Go and TypeScript.
for (const file of files) {
  const corpus = JSON.parse(readFileSync(new URL(file, directory), 'utf8'));
  for (const example of corpus.cases) {
    test(`${corpus.family}: ${example.name}`, () => {
      const base = JSON.stringify(scenarioFixture());
      const native = base.replace('"fixed":{', `"fixed":{"randomProfiles":{"case":${example.document}},`);
      if (!example.valid) {
        assert.throws(() => parseRandomProfile(example.document));
        assert.throws(() => parseScenario(native));
        return;
      }
      const profile = parseRandomProfile(example.document);
      assert.deepEqual(parseRandomProfile(serializeRandomProfile(profile)), profile);
      assert.deepEqual(parseScenario(native).fixed.randomProfiles?.case, profile);
    });
  }
}
