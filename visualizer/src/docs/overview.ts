import GithubSlugger from 'github-slugger';
import { toString } from 'mdast-util-to-string';
import type { Heading, Nodes } from 'mdast';
import remarkGfm from 'remark-gfm';
import remarkParse from 'remark-parse';
import { unified } from 'unified';

export const overviewPages = [
  { id: 'introduction', title: 'Introduction', path: '/docs' },
  { id: 'setup', title: 'Installation and running', path: '/docs/setup' },
  { id: 'scenarios', title: 'Scenarios and configuration', path: '/docs/scenarios' },
  { id: 'simulation', title: 'Simulation model', path: '/docs/simulation' },
  { id: 'tcp', title: 'TCP and connections', path: '/docs/tcp' },
  { id: 'metrics', title: 'Metrics and interface', path: '/docs/metrics' },
  { id: 'verification', title: 'Verification', path: '/docs/verification' },
] as const;

export type OverviewPageId = typeof overviewPages[number]['id'];
export type OverviewDocument = typeof overviewPages[number] & { markdown: string };

const parser = unified().use(remarkParse).use(remarkGfm);
const markers: ReadonlyArray<{
  pageId: OverviewPageId;
  type: 'heading' | 'paragraph';
  text: string;
}> = [
  { pageId: 'setup', type: 'heading', text: 'Run with Docker Compose' },
  { pageId: 'scenarios', type: 'heading', text: 'Scenario configuration foundation' },
  { pageId: 'metrics', type: 'heading', text: 'Live telemetry' },
  { pageId: 'simulation', type: 'heading', text: 'Virtual-time architecture' },
  { pageId: 'tcp', type: 'paragraph', text: 'The current **byte-based sliding-window model**' },
  { pageId: 'setup', type: 'heading', text: 'Run the visualizer' },
  { pageId: 'metrics', type: 'heading', text: 'Simulation and topology pages' },
  { pageId: 'verification', type: 'heading', text: 'Verify the project' },
];

function visitHeadings(node: Nodes, visit: (heading: Heading) => void) {
  if (node.type === 'heading') visit(node);
  if ('children' in node) {
    for (const child of node.children) visitHeadings(child, visit);
  }
}

export function splitOverview(source: string): {
  pages: OverviewDocument[];
  anchorLinks: ReadonlyMap<string, string>;
} {
  const tree = parser.parse(source);
  const starts = markers.map(marker => {
    const matches = tree.children.filter(node => {
      if (marker.type === 'heading') {
        return node.type === 'heading' && node.depth === 2 && toString(node) === marker.text;
      }
      return node.type === 'paragraph'
        && source.slice(node.position?.start.offset, node.position?.end.offset).startsWith(marker.text);
    });
    if (matches.length !== 1) {
      throw new Error(`Overview requires exactly one top-level ${marker.type} marker "${marker.text}"; found ${matches.length}.`);
    }
    const offset = matches[0].position?.start.offset;
    if (offset === undefined) throw new Error(`Overview marker "${marker.text}" has no source offset.`);
    return { offset, pageId: marker.pageId };
  }).sort((left, right) => left.offset - right.offset);
  starts.unshift({ offset: 0, pageId: 'introduction' });

  // Slicing at parser offsets keeps every original character, including the
  // whitespace between sections and the README's original line endings.
  const fragments: Partial<Record<OverviewPageId, string>> = {};
  for (const [index, start] of starts.entries()) {
    const end = starts[index + 1]?.offset ?? source.length;
    fragments[start.pageId] = (fragments[start.pageId] ?? '') + source.slice(start.offset, end);
  }
  const pages = overviewPages.map(page => ({ ...page, markdown: fragments[page.id] ?? '' }));

  // Each page renders its own heading IDs. Keep the original document's IDs
  // usable even when duplicate headings now live on different pages.
  const originalSlugger = new GithubSlugger();
  const pageSluggers = new Map<OverviewPageId, GithubSlugger>();
  const anchorLinks = new Map<string, string>();
  let section = 0;
  for (const node of tree.children) {
    const offset = node.position?.start.offset ?? 0;
    while (starts[section + 1] && starts[section + 1].offset <= offset) section += 1;
    const pageId = starts[section].pageId;
    const page = overviewPages.find(candidate => candidate.id === pageId)!;
    const pageSlugger = pageSluggers.get(pageId) ?? new GithubSlugger();
    pageSluggers.set(pageId, pageSlugger);
    visitHeadings(node, heading => {
      const text = toString(heading);
      anchorLinks.set(originalSlugger.slug(text), `${page.path}#${pageSlugger.slug(text)}`);
    });
  }

  return { pages, anchorLinks };
}
