import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import test from 'node:test';
import GithubSlugger from 'github-slugger';
import type { Heading, Link, Nodes } from 'mdast';
import { toString } from 'mdast-util-to-string';
import remarkGfm from 'remark-gfm';
import remarkParse from 'remark-parse';
import { unified } from 'unified';
import { documentationConcepts, remarkConceptBacklinks, remarkConceptLinks } from '../../../src/docs/concepts.ts';
import { languagePages } from '../../../src/docs/language.ts';
import { splitOverview } from '../../../src/docs/overview.ts';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { documentationExamples, wrapDocumentationExample } from '../../documentationExamples.ts';

const parser = unified().use(remarkParse).use(remarkGfm);
const overview = splitOverview(readFileSync(new URL('../../../../README.md', import.meta.url), 'utf8'));
const documents = [...overview.pages, ...languagePages];

function descendants(node: Nodes): Nodes[] {
  return [node, ...('children' in node ? node.children.flatMap(descendants) : [])];
}

function links(node: Nodes): Link[] {
  return descendants(node).filter((item): item is Link => item.type === 'link');
}

function headingIds(markdown: string): Set<string> {
  const slugger = new GithubSlugger();
  return new Set(descendants(parser.parse(markdown))
    .filter((node): node is Heading => node.type === 'heading')
    .map(node => slugger.slug(toString(node))));
}

const anchors = new Map(documents.map(page => [page.path, headingIds(page.markdown)]));

function assertDestination(destination: string, source: string) {
  const [path, hash] = destination.split('#');
  assert.ok(anchors.has(path), `${source}: unknown documentation page ${path}`);
  if (hash !== undefined) {
    assert.ok(anchors.get(path)?.has(decodeURIComponent(hash)), `${source}: missing anchor ${destination}`);
  }
}

test('all nine authored language pages expose a summary and a working table of contents at the top', () => {
  const ids = ['start', 'syntax', 'topology', 'clients', 'defaults', 'tcp', 'timeline', 'examples', 'errors'];
  assert.deepEqual(languagePages.map(page => page.id), ids);
  assert.deepEqual(
    readdirSync(new URL('../../../docs/language/', import.meta.url)).filter(name => name.endsWith('.md')).sort(),
    ids.map(id => `${id}.md`).sort(),
  );
  for (const page of languagePages) {
    assert.equal(page.markdown, readFileSync(new URL(`../../../docs/language/${page.id}.md`, import.meta.url), 'utf8'));
    const children = parser.parse(page.markdown).children;
    assert.equal(children[0].type, 'heading', `${page.id}: begins with a title`);
    assert.equal(children[0].type === 'heading' && children[0].depth, 1, page.id);
    assert.equal(toString(children[0]), page.title, `${page.id}: navigation uses the chapter title`);
    const sectionHeadings = children.filter((node): node is Heading => node.type === 'heading' && node.depth === 2);
    assert.equal(toString(sectionHeadings[0]), 'Summary', `${page.id}: summary precedes the reading guide`);
    const summary = children.findIndex(node => node.type === 'heading' && toString(node) === 'Summary');
    const toc = children.findIndex(node => node.type === 'heading' && toString(node) === 'On this page');
    assert.ok(toc > summary, `${page.id}: explicit On this page heading follows the summary`);
    assert.ok(children.slice(summary + 1, toc).some(node => node.type !== 'heading' && toString(node).trim()), `${page.id}: summary has content`);
    assert.ok(children.slice(0, toc).every(node => node.type !== 'code' && node.type !== 'table'), `${page.id}: navigation precedes examples and reference tables`);
    assert.ok(children.slice(0, toc).filter(node => node.type === 'heading' && node.depth === 2)
      .every(node => ['Summary', 'Language guide'].includes(toString(node))), `${page.id}: navigation precedes topic sections`);
    const nextSection = children.findIndex((node, index) => index > toc && node.type === 'heading');
    assert.ok(nextSection > toc, `${page.id}: includes topic content`);
    const localLinks = children.slice(toc + 1, nextSection).flatMap(links).filter(link => link.url.startsWith('#'));
    assert.ok(localLinks.length > 0, `${page.id}: local table of contents has section links`);
    const targetIds = new Set(localLinks.map(link => link.url.slice(1)));
    const slugger = new GithubSlugger();
    for (const node of descendants(parser.parse(page.markdown))) {
      if (node.type !== 'heading') continue;
      const id = slugger.slug(toString(node));
      if (node.depth === 2 && (node.position?.start.offset ?? 0) > (children[toc].position?.start.offset ?? 0)) {
        assert.ok(targetIds.has(id), `${page.id}: TOC includes ${toString(node)}`);
      }
    }
  }
  const first = languagePages[0];
  const destinations = links(parser.parse(first.markdown)).map(link => link.url);
  for (const page of languagePages) assert.ok(destinations.includes(page.path), `Start here links to ${page.id}`);
});

test('every documented valid example compiles and every invalid example reports its promised error', () => {
  const examples = documentationExamples();
  assert.ok(examples.some(example => example.language === 'lb-invalid'), 'the error guide checks real failures');
  for (const page of languagePages) assert.ok(examples.some(example => example.page === page.id), `${page.id}: has checked examples`);
  for (const example of examples) {
    const source = wrapDocumentationExample(example.code, example.context);
    if (example.language === 'lb-invalid') {
      assert.throws(
        () => compileScenario(source),
        (error: unknown) => error instanceof Error && error.message.includes(example.expectedError!),
        example.name,
      );
    } else {
      assert.doesNotThrow(() => compileScenario(source), example.name);
    }
  }
});

test('authored section links, language navigation and concept links resolve to rendered documentation headings', () => {
  for (const page of documents) {
    const processor = unified().use(remarkParse).use(remarkGfm).use(remarkConceptLinks);
    const tree = processor.runSync(processor.parse(page.markdown));
    for (const link of links(tree)) {
      if (link.url.startsWith('#')) {
        const hash = link.url.slice(1);
        const isOverview = overview.pages.some(item => item.path === page.path);
        const destination = isOverview ? overview.anchorLinks.get(hash) ?? `${page.path}${link.url}` : `${page.path}${link.url}`;
        assertDestination(destination, `${page.id}: ${toString(link)}`);
      } else if (/^\/docs(?:\/|#|$)/.test(link.url)) {
        assertDestination(link.url, `${page.id}: ${toString(link)}`);
      }
    }
  }
});

test('each central concept targets an existing explanation and inserts a configuration backlink at that heading', () => {
  assert.equal(new Set(documentationConcepts.map(concept => concept.id)).size, documentationConcepts.length);
  for (const concept of documentationConcepts) {
    assertDestination(concept.overview, `${concept.id}: explanation`);
    assertDestination(concept.language, `${concept.id}: configuration`);
  }
  for (const page of overview.pages) {
    const processor = unified().use(remarkParse).use(remarkGfm).use(remarkConceptBacklinks, { path: page.path });
    const tree = processor.runSync(processor.parse(page.markdown));
    const slugger = new GithubSlugger();
    let inserted = 0;
    for (const [index, node] of tree.children.entries()) {
      if (node.type !== 'heading') continue;
      const destination = `${page.path}#${slugger.slug(toString(node))}`;
      const expected = documentationConcepts.filter(concept => concept.overview === destination);
      for (const [offset, concept] of expected.entries()) {
        const backlink = tree.children[index + offset + 1];
        assert.equal(backlink.type, 'paragraph', `${concept.id}: backlink follows its heading`);
        assert.deepEqual(links(backlink).map(link => link.url), [concept.language], concept.id);
        assert.match(toString(backlink), /^Configure /, concept.id);
        inserted++;
      }
    }
    assert.equal(inserted, documentationConcepts.filter(concept => concept.overview.startsWith(`${page.path}#`)).length, page.id);
  }
});

test('concept plugins reject unknown keys and keep backlinks scoped to a page and its unique heading IDs', () => {
  const resolver = unified().use(remarkParse).use(remarkGfm).use(remarkConceptLinks);
  const source = documentationConcepts.map(concept => `[Explain ${concept.label}](concept:${concept.id})`).join('\n\n');
  const resolved = resolver.runSync(resolver.parse(source));
  assert.deepEqual(links(resolved).map(link => link.url), documentationConcepts.map(concept => concept.overview));
  assert.throws(() => resolver.runSync(resolver.parse('[Unknown](concept:unknown)')), /Unknown documentation concept: unknown/);

  const repeated = '### Selective acknowledgments\n\nFirst explanation.\n\n### Selective acknowledgments\n\nSecond explanation.';
  const expected = documentationConcepts.find(concept => concept.id === 'sack')!;
  const processor = unified().use(remarkParse).use(remarkGfm).use(remarkConceptBacklinks, { path: '/docs/tcp' });
  const tree = processor.runSync(processor.parse(repeated));
  assert.deepEqual(links(tree).map(link => link.url), [expected.language]);
  assert.equal(toString(tree.children[1]).startsWith('Configure '), true);
  const secondHeading = tree.children.findIndex((node, index) => index > 0 && node.type === 'heading');
  assert.equal(toString(tree.children[secondHeading + 1]), 'Second explanation.');
  const unrelated = unified().use(remarkParse).use(remarkGfm).use(remarkConceptBacklinks, { path: '/docs/simulation' });
  assert.equal(links(unrelated.runSync(unrelated.parse(repeated))).length, 0);
});
