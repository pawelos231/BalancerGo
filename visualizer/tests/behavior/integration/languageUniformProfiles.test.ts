import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { checkLanguageSource } from '../../../src/scenario-language/diagnostics.ts';
import { loadLanguageSource, serializeLanguageSource, validateLanguageDocument } from '../../../src/scenario-language/editor.ts';
import { serializeScenario } from '../../../src/scenarios/files.ts';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';
import { sourceExamples } from '../../../src/scenarios/sourceExamples.ts';

const base = 'run 2s speed 0\nbalancer lb {}\nbackend api-0 {}\n';

// The library's canonical source reaches the same native JSON used by files and the API.
test('named uniform definitions compile and reach native admission unchanged', async () => {
  const example = sourceExamples.find(entry => entry.id === 'uniform-profile-definitions')!;
  const scenario = compileScenario(example.source);
  assert.equal(example.expectedDiagnostic, undefined);
  assert.equal(checkLanguageSource(example.source), null);
  assert.deepEqual({ ...scenario.fixed.randomProfiles }, {
    'cpu-time': { kind: 'uniform', unit: 'nanoseconds', min: '5000', max: '15000' },
    'travel-time': { kind: 'uniform', unit: 'nanoseconds', min: '500000', max: '1000000' },
    'precise-bytes': {
      kind: 'uniform', unit: 'bytes', min: '9007199254740992.125', max: '9007199254740992.375',
    },
  });

  const document = serializeScenario(scenario);
  assert.equal(serializeScenario(parseScenario(document)), document);
  let requests = 0;
  const checked = await validateLanguageDocument(example.source, {
    validate: async input => {
      requests++;
      assert.equal(serializeScenario(input), document);
      return {
        scenario: validateScenario(input),
        summary: { totalClients: 2, backendCount: 1, clientGroupCount: 1 },
      };
    },
  }, new AbortController().signal);
  assert.equal(requests, 1);
  assert.equal(serializeScenario(checked.scenario), document);
});

test('named uniform source exports and imports without changing its text or diagnostics', async () => {
  const example = sourceExamples.find(entry => entry.id === 'uniform-profile-definitions')!;
  const exported = serializeLanguageSource(example.source);
  const loaded = await loadLanguageSource({
    size: new TextEncoder().encode(exported).length,
    text: async () => exported,
  }, new AbortController().signal);
  assert.equal(loaded.source, example.source);
  assert.equal(loaded.diagnostic, null);
});

// Only the definitions are new; the same topology continues using its original scalars.
test('unbound definitions preserve scalar settings and do not populate old documents', () => {
  const old = compileScenario(base);
  const named = compileScenario(`${base}distribution cost uniform { min 5us max 15us }`);
  assert.equal(Object.hasOwn(old.fixed, 'randomProfiles'), false);
  delete named.fixed.randomProfiles;
  assert.deepEqual(named, old);
});

// Conversion preserves sub-nanosecond fractions and precision beyond Number.MAX_SAFE_INTEGER.
test('named uniform bounds convert units exactly without drawing or rounding', () => {
  const scenario = compileScenario(`${base}
distribution tiny uniform { min 0.000125us max 0.001us }
distribution precise uniform { min 9007199254740992.125B max 9007199254740992.375B }
distribution percent uniform { min 1.50% max 2.0% }
distribution plain uniform { min 0.10 max 0.20 }
`);
  assert.deepEqual({ ...scenario.fixed.randomProfiles }, {
    tiny: { kind: 'uniform', unit: 'nanoseconds', min: '0.125', max: '1' },
    precise: { kind: 'uniform', unit: 'bytes', min: '9007199254740992.125', max: '9007199254740992.375' },
    percent: { kind: 'uniform', unit: 'percentage', min: '1.5', max: '2' },
    plain: { kind: 'uniform', unit: 'number', min: '0.1', max: '0.2' },
  });
});

// A declaration must stand on its own before at blocks; reuse and runtime binding come later.
test('named uniform declarations reject duplicate names, unknown families and bad parameters', async context => {
  const cases = [
    {
      name: 'duplicate distribution names',
      source: 'distribution cost uniform { min 5us max 15us }\ndistribution cost uniform { min 1us max 2us }',
      message: /Duplicate distribution cost/,
    },
    {
      name: 'a distribution name with a numeric segment',
      source: 'distribution bad-1 uniform { min 1 max 2 }',
      message: /lowercase name/,
    },
    {
      name: 'an unsupported distribution family',
      source: 'distribution cost normal { min 1 max 2 }',
      message: /Unsupported distribution/,
    },
    {
      name: 'reversed uniform bounds',
      source: 'distribution cost uniform { min 2 max 1 }',
      message: /min must be less than max/,
    },
    {
      name: 'incompatible bound units',
      source: 'distribution cost uniform { min 1us max 2B }',
      message: /compatible units/,
    },
    {
      name: 'a missing maximum bound',
      source: 'distribution cost uniform { min 1 }',
      message: /both min and max/,
    },
    {
      name: 'a duplicate maximum parameter',
      source: 'distribution cost uniform { min 1 max 2 max 3 }',
      message: /Duplicate uniform parameter/,
    },
    {
      name: 'an unknown uniform parameter',
      source: 'distribution cost uniform { min 1 max 2 extra 3 }',
      message: /Unknown uniform parameter/,
    },
    {
      name: 'a declaration after a timeline block',
      source: 'at 1s {}\ndistribution cost uniform { min 1 max 2 }',
      message: /Initial declarations must precede/,
    },
  ];

  for (const fixture of cases) {
    await context.test(fixture.name, () => {
      assert.throws(() => compileScenario(base + fixture.source), fixture.message);
    });
  }
});

// Compiler output matches the fixture shared with Go admission.
test('canonical named source matches its shared native JSON fixture', () => {
  const source = sourceExamples.find(entry => entry.id === 'uniform-profile-definitions')!.source;
  const expected = readFileSync(new URL('../../../../examples/randomness/uniform-scenario.json', import.meta.url), 'utf8');
  assert.equal(serializeScenario(compileScenario(source)), expected);
});
