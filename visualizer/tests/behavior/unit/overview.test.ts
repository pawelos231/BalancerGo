import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import GithubSlugger from 'github-slugger';
import { toString } from 'mdast-util-to-string';
import type { Heading, Nodes } from 'mdast';
import remarkGfm from 'remark-gfm';
import remarkParse from 'remark-parse';
import { unified } from 'unified';
import { overviewPages, splitOverview } from '../../../src/docs/overview.ts';

const readme = readFileSync(new URL('../../../../README.md', import.meta.url), 'utf8');
const parser = unified().use(remarkParse).use(remarkGfm);

function serializedNodes(source: string) {
  return parser.parse(source).children.map(node => JSON.stringify(node, (key, value) => (
    key === 'position' ? undefined : value
  )));
}

function headings(node: Nodes): Heading[] {
  if (node.type === 'heading') return [node];
  return 'children' in node ? node.children.flatMap(headings) : [];
}

test('overview partitions the complete README without changing its Markdown content', () => {
  const { pages } = splitOverview(readme);
  assert.deepEqual(pages.map(({ markdown, ...page }) => {
    assert.ok(markdown.trim(), `${page.id} has content`);
    return page;
  }), overviewPages);
  assert.equal(pages.reduce((length, page) => length + page.markdown.length, 0), readme.length);
  assert.deepEqual(
    pages.flatMap(page => serializedNodes(page.markdown)).sort(),
    serializedNodes(readme).sort(),
  );

  const expectedContent = {
    introduction: ['pytanie naukowe:', '## Code organization'],
    setup: ['## Run with Docker Compose', '## Run the simulator', '## Run the visualizer'],
    scenarios: ['## Scenario configuration foundation', '## Scenario HTTP API', '## Scenario files and effective configuration'],
    simulation: ['## Virtual-time architecture', 'The **LB forwarding budget**'],
    tcp: ['The current **byte-based sliding-window model**', '### TCP connection closing'],
    metrics: ['## Live telemetry', '## Simulation and topology pages', '**Payload progress**'],
    verification: ['## Verify the project', 'go test ./...'],
  };
  for (const page of pages) {
    for (const content of expectedContent[page.id]) {
      assert.ok(page.markdown.includes(content), `${page.id} includes ${content}`);
      assert.equal(pages.filter(other => other.markdown.includes(content)).length, 1);
    }
  }
});

test('every original README heading routes to an existing heading on the correct page', () => {
  const { pages, anchorLinks } = splitOverview(readme);
  const originalSlugs = new GithubSlugger();
  const originalHeadings = headings(parser.parse(readme));
  assert.equal(anchorLinks.size, originalHeadings.length);
  const renderedHeadings = new Map(pages.map(page => {
    const slugger = new GithubSlugger();
    return [page.path, new Map(headings(parser.parse(page.markdown)).map(heading => (
      [slugger.slug(toString(heading)), toString(heading)]
    )))];
  }));
  for (const heading of originalHeadings) {
    const text = toString(heading);
    const destination = anchorLinks.get(originalSlugs.slug(text));
    assert.ok(destination, `route for ${text}`);
    const [path, id] = destination.split('#');
    assert.equal(renderedHeadings.get(path)?.get(id), text);
  }
  assert.equal(anchorLinks.get('run-the-visualizer'), '/docs/setup#run-the-visualizer');
  assert.equal(anchorLinks.get('tcp-connection-closing'), '/docs/tcp#tcp-connection-closing');

  const duplicated = splitOverview(`### Shared heading\n\n${readme.replace(
    '## Run with Docker Compose', '## Run with Docker Compose\n\n### Shared heading',
  )}`);
  assert.equal(duplicated.anchorLinks.get('shared-heading'), '/docs#shared-heading');
  assert.equal(duplicated.anchorLinks.get('shared-heading-1'), '/docs/setup#shared-heading');
});

test('only real top-level markers split pages, and missing or duplicate markers fail clearly', () => {
  const fenced = '```markdown\n## Run with Docker Compose\nThe current **byte-based sliding-window model**\n```\n\n';
  const quoted = '> ## Run with Docker Compose\n\n';
  const { pages } = splitOverview(fenced + quoted + readme);
  assert.ok(pages[0].markdown.startsWith(fenced + quoted));
  assert.ok(pages.find(page => page.id === 'setup')?.markdown.startsWith('## Run with Docker Compose'));
  assert.throws(
    () => splitOverview(readme.replace('## Run with Docker Compose', '## Renamed installation')),
    /exactly one top-level heading marker "Run with Docker Compose"; found 0/,
  );
  assert.throws(
    () => splitOverview(`${readme}\n## Run with Docker Compose\n`),
    /exactly one top-level heading marker "Run with Docker Compose"; found 2/,
  );
  assert.throws(
    () => splitOverview(readme.replace('The current **byte-based sliding-window model**', 'A renamed TCP model')),
    /exactly one top-level paragraph marker "The current \*\*byte-based sliding-window model\*\*"; found 0/,
  );
});
