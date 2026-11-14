// The reading order is also the sidebar order; each page has one authored source.
export const languagePages = [
  {
    id: 'start', title: 'Start here', path: '/docs/language',
    loadMarkdown: () => import('../../docs/language/start.md?raw').then(module => module.default),
  },
  {
    id: 'syntax', title: 'Syntax and units', path: '/docs/language/syntax',
    loadMarkdown: () => import('../../docs/language/syntax.md?raw').then(module => module.default),
  },
  {
    id: 'topology', title: 'Topology and capacity', path: '/docs/language/topology',
    loadMarkdown: () => import('../../docs/language/topology.md?raw').then(module => module.default),
  },
  {
    id: 'clients', title: 'Clients and traffic', path: '/docs/language/clients',
    loadMarkdown: () => import('../../docs/language/clients.md?raw').then(module => module.default),
  },
  {
    id: 'defaults', title: 'Defaults, profiles and overrides', path: '/docs/language/defaults',
    loadMarkdown: () => import('../../docs/language/defaults.md?raw').then(module => module.default),
  },
  {
    id: 'tcp', title: 'TCP settings', path: '/docs/language/tcp',
    loadMarkdown: () => import('../../docs/language/tcp.md?raw').then(module => module.default),
  },
  {
    id: 'timeline', title: 'Timeline changes', path: '/docs/language/timeline',
    loadMarkdown: () => import('../../docs/language/timeline.md?raw').then(module => module.default),
  },
  {
    id: 'examples', title: 'Small experiments', path: '/docs/language/examples',
    loadMarkdown: () => import('../../docs/language/examples.md?raw').then(module => module.default),
  },
  {
    id: 'errors', title: 'Fixing errors', path: '/docs/language/errors',
    loadMarkdown: () => import('../../docs/language/errors.md?raw').then(module => module.default),
  },
] as const;

export type LanguagePageId = typeof languagePages[number]['id'];
export type LanguageDocument = typeof languagePages[number];
