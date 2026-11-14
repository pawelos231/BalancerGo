import assert from 'node:assert/strict';
import test from 'node:test';
import { EditorState } from '@codemirror/state';
import { ensureSyntaxTree } from '@codemirror/language';
import { highlightTree, tagHighlighter, tags } from '@lezer/highlight';
import { scenarioLanguage } from '../../../src/components/scenarioLanguageHighlighting.ts';

function highlighted(source: string) {
  const state = EditorState.create({ doc: source, extensions: [scenarioLanguage] });
  const tree = ensureSyntaxTree(state, state.doc.length, 1000);
  assert.ok(tree);
  const tokens: { text: string; style: string }[] = [];
  const highlighter = tagHighlighter([
    { tag: tags.keyword, class: 'keyword' },
    { tag: tags.variableName, class: 'name' },
    { tag: tags.bool, class: 'bool' },
    { tag: tags.null, class: 'null' },
    { tag: tags.number, class: 'number' },
    { tag: tags.string, class: 'string' },
    { tag: tags.comment, class: 'comment' },
  ]);
  highlightTree(tree, highlighter, (from, to, style) => {
    tokens.push({ text: state.doc.sliceString(from, to), style });
  });
  return tokens;
}

test('language highlighting distinguishes colliding names and values in nested static blocks', () => {
  const source = `language 1
run 10s seed 1
profile queue { queue 32 }
balancer queue { host { cores 2 } }
backend api-0 use queue {
    admin drain
    name "API #1 \\"fast\\"" # comment
    available true
    to-lb { link null }
}
clients payload { count 1 use queue }
tcp {
    SACK true
    ECN false
    congestion-control { fast-retransmit null }
}
`.replace(/\n/g, '\r\n');
  const tokens = highlighted(source);
  const collisions = new Set(['queue', 'payload', 'drain', 'true', 'false', 'null']);
  assert.deepEqual(tokens.filter(token => collisions.has(token.text)), [
    { text: 'queue', style: 'name' },
    { text: 'queue', style: 'keyword' },
    { text: 'queue', style: 'name' },
    { text: 'queue', style: 'name' },
    { text: 'drain', style: 'name' },
    { text: 'true', style: 'bool' },
    { text: 'null', style: 'null' },
    { text: 'payload', style: 'name' },
    { text: 'queue', style: 'name' },
    { text: 'true', style: 'bool' },
    { text: 'false', style: 'bool' },
    { text: 'null', style: 'null' },
  ]);
  assert.deepEqual(tokens.filter(token => token.style === 'string' || token.style === 'comment'), [
    { text: '"API #1 \\"fast\\""', style: 'string' },
    { text: '# comment', style: 'comment' },
  ]);
  assert.ok(tokens.some(token => token.text === 'cores' && token.style === 'keyword'));
  assert.ok(tokens.some(token => token.text === 'SACK' && token.style === 'keyword'));
  assert.ok(tokens.some(token => token.text === 'ECN' && token.style === 'keyword'));
});

test('language highlighting restores statement and field roles across timeline commands', () => {
  const tokens = highlighted(`at 1s {
    set balancer { host { cores 2 } }
    set backend api-0 admin drain
    set clients count payload 2B
    set client count-0 payload null
    add clients payload use queue { count 1 }
    add backend api-1 { admin drain }
    add backends available { count 1 }
    down backend api-0
    up backend api-0
    drain backend api-0
    stop arrivals count
    remove clients payload
}
`);
  assert.deepEqual(tokens.filter(token => token.style === 'keyword').map(token => token.text), [
    'at', 'set', 'balancer', 'host', 'cores',
    'set', 'backend', 'admin',
    'set', 'clients', 'payload',
    'set', 'client', 'payload',
    'add', 'clients', 'use', 'count',
    'add', 'backend', 'admin',
    'add', 'backends', 'count',
    'down', 'backend', 'up', 'backend', 'drain', 'backend',
    'stop', 'arrivals', 'remove', 'clients',
  ]);
  assert.deepEqual(tokens.filter(token => token.style === 'name').map(token => token.text), [
    'api-0', 'drain', 'count', 'count-0', 'payload', 'queue',
    'api-1', 'drain', 'available', 'api-0', 'api-0', 'api-0', 'count', 'payload',
  ]);
  assert.ok(tokens.some(token => token.text === 'null' && token.style === 'null'));
});
