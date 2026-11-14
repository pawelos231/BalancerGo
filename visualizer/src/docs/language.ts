import start from '../../docs/language/start.md?raw';
import syntax from '../../docs/language/syntax.md?raw';
import topology from '../../docs/language/topology.md?raw';
import clients from '../../docs/language/clients.md?raw';
import defaults from '../../docs/language/defaults.md?raw';
import tcp from '../../docs/language/tcp.md?raw';
import timeline from '../../docs/language/timeline.md?raw';
import examples from '../../docs/language/examples.md?raw';
import errors from '../../docs/language/errors.md?raw';

// The reading order is also the sidebar order; each page has one authored source.
export const languagePages = [
  { id: 'start', title: 'Start here', path: '/docs/language', markdown: start },
  { id: 'syntax', title: 'Syntax and units', path: '/docs/language/syntax', markdown: syntax },
  { id: 'topology', title: 'Topology and capacity', path: '/docs/language/topology', markdown: topology },
  { id: 'clients', title: 'Clients and traffic', path: '/docs/language/clients', markdown: clients },
  { id: 'defaults', title: 'Defaults, profiles and overrides', path: '/docs/language/defaults', markdown: defaults },
  { id: 'tcp', title: 'TCP settings', path: '/docs/language/tcp', markdown: tcp },
  { id: 'timeline', title: 'Timeline changes', path: '/docs/language/timeline', markdown: timeline },
  { id: 'examples', title: 'Small experiments', path: '/docs/language/examples', markdown: examples },
  { id: 'errors', title: 'Fixing errors', path: '/docs/language/errors', markdown: errors },
] as const;

export type LanguagePageId = typeof languagePages[number]['id'];
export type LanguageDocument = typeof languagePages[number];
