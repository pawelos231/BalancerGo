import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { parseScenario, validateScenario } from '../../../src/scenarios/validation.ts';

interface ValidationCase {
  name: string;
  valid: boolean;
  errorField?: string;
  changes?: { path: string[]; value: unknown }[];
  replacements?: { from: string; to: string }[];
}

const corpusURL = new URL('../../../../examples/scenarios/validation/cases.json', import.meta.url);
const corpus = JSON.parse(readFileSync(corpusURL, 'utf8')) as {
  base: string;
  cases: ValidationCase[];
};
const baseText = readFileSync(new URL(corpus.base, corpusURL), 'utf8');

// Keep this small path editor identical in purpose to the Go corpus reader.
function fixtureDocument(example: ValidationCase): string {
  const document = JSON.parse(baseText) as Record<string, unknown>;

  for (const change of example.changes ?? []) {
    let parent = document;
    for (const key of change.path.slice(0, -1)) {
      parent = parent[key] as Record<string, unknown>;
    }
    parent[change.path[change.path.length - 1]] = structuredClone(change.value);
  }

  let text = JSON.stringify(document);
  for (const replacement of example.replacements ?? []) {
    assert.ok(text.includes(replacement.from), `fixture replacement did not match ${replacement.from}`);
    text = text.replace(replacement.from, replacement.to);
  }
  return text;
}

// These same cases are checked with DecodeDocument + Normalize in Go.
for (const example of corpus.cases) {
  test(`shared scenario validation: ${example.name}`, () => {
    const document = fixtureDocument(example);

    if (example.valid) {
      assert.doesNotThrow(() => parseScenario(document));
    } else {
      assert.throws(() => parseScenario(document), (error: unknown) => {
        assert.ok(error instanceof Error);
        assert.ok(error.message.includes(example.errorField ?? ''), error.message);
        return true;
      });
    }
  });
}

// Quoted integers and exact map keys must survive without passing through Number.
test('scenario preserves exact integers and keeps timestamp order out of the JSON contract', () => {
  const example = corpus.cases.find((entry) => entry.name === 'large exact timestamps and integer strings');
  assert.ok(example);

  const input = parseScenario(fixtureDocument(example));
  assert.equal(input.fixed.execution.seed, '-9007199254740993');
  assert.equal(input.fixed.execution.maxEvents, '18446744073709551615');
  assert.ok(Object.hasOwn(input.states, '9007199254740993'));
  assert.doesNotThrow(() => parseScenario(JSON.stringify(input)));
});

// Go keeps a valid emoji pair and replaces each unmatched surrogate with U+FFFD.
test('scenario string decoding preserves pairs and replaces unmatched high and low surrogates', () => {
  const document = fixtureDocument({
    name: 'surrogate decoding',
    valid: true,
    replacements: [{
      from: '"name":"Web 1"',
      to: '"name":"\\ud83d\\ude80-\\ud800-\\udc00"',
    }],
  });

  const parsed = parseScenario(document);
  assert.equal(parsed.states['0'].resources.backends[0].name, '\u{1F680}-\uFFFD-\uFFFD');

  const checkedObject = validateScenario(JSON.parse(document));
  assert.equal(checkedObject.states['0'].resources.backends[0].name, parsed.states['0'].resources.backends[0].name);

  // Existing objects must also detect addresses that become identical after JSON decoding in Go.
  const duplicate = validateScenario(JSON.parse(baseText));
  duplicate.states['0'].resources.backends[0].address = '\ud800';
  duplicate.states['0'].resources.backends[1].address = '\ud801';
  assert.throws(() => validateScenario(duplicate), /already used/);
});

// Runtime callers also validate unknown input, without trusting TypeScript casts.
test('scenario validation rejects wrong input shapes and leaves its input untouched', () => {
  for (const input of [null, [], false, 'not a scenario', { scenarioVersion: 2 }]) {
    assert.throws(() => validateScenario(input));
  }

  const input = JSON.parse(baseText) as Record<string, unknown>;
  const original = structuredClone(input);
  const checked = validateScenario(input);
  assert.deepEqual(input, original);

  // Editing the validated copy must not change the original document.
  checked.states['0'].resources.backends[0].address = 'edited.example';
  assert.deepEqual(input, original);
});

// An absent JS array entry would serialize as null, which Go rejects for an override.
test('scenario rejects sparse or undefined override entries in existing objects', () => {
  for (const overrides of [Array(1), [undefined]]) {
    const input = validateScenario(JSON.parse(baseText));
    Object.assign(input.states['0'].workload.clientGroups[0], { overrides });
    assert.throws(() => validateScenario(input), /overrides\[0\]/);
  }
});

// Invalid JSON must fail during parsing, before a valid prefix can reach semantic checks.
test('scenario rejects incomplete documents, trailing JSON and trailing commas', () => {
  const complete = JSON.stringify(JSON.parse(baseText));
  const documents = [
    `${complete} {}`,
    complete.slice(0, -1),
    `${complete.slice(0, -1)},}`,
  ];
  for (const document of documents) {
    assert.throws(() => parseScenario(document), /character/);
  }
});

// Missing ordinary fields use Go's zero values; optional null is not an explicit zero.
test('scenario allows omitted zero-valued fields and null pointers, but rejects null objects', () => {
  const minimal = {
    scenarioVersion: 2,
    fixed: {
      execution: { durationNanoseconds: 10, snapshotIntervalNanoseconds: 1, maxEvents: 1 },
      loadBalancer: { id: 'lb' },
      network: { defaults: { bandwidthBitsPerSecond: 1 } },
    },
    states: {
      '0': {
        resources: {
          loadBalancer: {
            algorithm: 'roundRobin',
            ingress: { bandwidthBitsPerSecond: 1 },
            forwardingBitsPerSecond: 1,
          },
        },
      },
    },
  };
  assert.doesNotThrow(() => parseScenario(JSON.stringify(minimal)));

  const wrong = structuredClone(minimal) as Record<string, unknown>;
  wrong.fixed = null;
  assert.throws(() => validateScenario(wrong));
});
