import GithubSlugger from 'github-slugger';
import { toString } from 'mdast-util-to-string';
import type { Link, Nodes, Root, RootContent } from 'mdast';

// One catalog connects configuration instructions with the modeled behavior.
export const documentationConcepts = [
  {
    id: 'sack', label: 'SACK',
    overview: '/docs/tcp#selective-acknowledgments', language: '/docs/language/tcp#sack',
  },
  {
    id: 'rto', label: 'RTO',
    overview: '/docs/tcp#rtt-and-retransmission-timeouts', language: '/docs/language/tcp#rto',
  },
  {
    id: 'mss', label: 'MSS',
    overview: '/docs/tcp#segment-size-and-mss', language: '/docs/language/tcp#mss',
  },
  {
    id: 'rwnd', label: 'receive window',
    overview: '/docs/tcp#receive-window', language: '/docs/language/tcp#receive-window',
  },
  {
    id: 'congestion', label: 'congestion control',
    overview: '/docs/tcp#congestion-control', language: '/docs/language/tcp#congestion-control',
  },
  {
    id: 'ecn', label: 'ECN',
    overview: '/docs/tcp#ecn', language: '/docs/language/tcp#ecn',
  },
  {
    id: 'delayed-ack', label: 'delayed ACK',
    overview: '/docs/tcp#delayed-ack', language: '/docs/language/tcp#delayed-ack',
  },
  {
    id: 'closing', label: 'TCP closing',
    overview: '/docs/tcp#tcp-connection-closing', language: '/docs/language/tcp#closing',
  },
  {
    id: 'links', label: 'endpoint links',
    overview: '/docs/simulation#links-and-packet-travel', language: '/docs/language/topology#endpoint-links',
  },
  {
    id: 'ingress', label: 'ingress',
    overview: '/docs/simulation#shared-ingress', language: '/docs/language/topology#shared-lb-limits',
  },
  {
    id: 'egress', label: 'egress',
    overview: '/docs/simulation#forwarding-budget', language: '/docs/language/topology#shared-lb-limits',
  },
  {
    id: 'queues', label: 'network queues',
    overview: '/docs/simulation#network-queues', language: '/docs/language/topology#queues',
  },
  {
    id: 'cpu', label: 'host CPU',
    overview: '/docs/scenarios#mutable-virtual-cpus', language: '/docs/language/topology#cpu',
  },
  {
    id: 'backend-lifecycle', label: 'backend availability',
    overview: '/docs/tcp#backend-availability-and-drain', language: '/docs/language/timeline#backend-availability',
  },
  {
    id: 'virtual-time', label: 'virtual time',
    overview: '/docs/simulation#virtual-time-architecture', language: '/docs/language/syntax#time-and-playback',
  },
  {
    id: 'client-lifecycle', label: 'client lifecycle',
    overview: '/docs/tcp#independent-endpoint-lifecycle', language: '/docs/language/clients#lifetime-client-budget',
  },
] as const;

function visitLinks(node: Nodes, visit: (link: Link) => void) {
  if (node.type === 'link') visit(node);
  if ('children' in node) {
    for (const child of node.children) visitLinks(child, visit);
  }
}

// Authors use concept:sack; readers receive a normal link to the exact section.
export function remarkConceptLinks() {
  return (tree: Root) => {
    visitLinks(tree, link => {
      if (!link.url.startsWith('concept:')) return;
      const id = link.url.slice('concept:'.length);
      const concept = documentationConcepts.find(item => item.id === id);
      if (!concept) throw new Error(`Unknown documentation concept: ${id}`);
      link.url = concept.overview;
    });
  };
}

// Put a configuration link immediately below its explanation's heading.
export function remarkConceptBacklinks({ path }: { path?: string }) {
  return (tree: Root) => {
    if (!path) return;
    const slugger = new GithubSlugger();
    tree.children = tree.children.flatMap<RootContent>(node => {
      if (node.type !== 'heading') return [node];
      const target = `${path}#${slugger.slug(toString(node))}`;
      const concepts = documentationConcepts.filter(item => item.overview === target);
      return [node, ...concepts.map(concept => ({
        type: 'paragraph' as const,
        children: [{
          type: 'link' as const, url: concept.language,
          children: [{ type: 'text' as const, value: `Configure ${concept.label} in the language →` }],
        }],
      }))];
    });
  };
}
