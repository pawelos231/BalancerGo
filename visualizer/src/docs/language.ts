import { distributionPages } from './distributions.ts';

// Keep the reading order stable when the sidebar groups related chapters.
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
    id: 'uniform', title: 'Uniform distributions', path: '/docs/language/uniform',
    loadMarkdown: () => import('../../docs/language/uniform.md?raw').then(module => module.default),
  },
  ...distributionPages,
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

function navigationPage(id: LanguagePageId, label?: string) {
  const page = languagePages.find(page => page.id === id)!;
  return { id: page.id, title: label ?? page.title, path: page.path };
}

const randomnessSections = [
  {
    id: 'continuous', title: 'Continuous', pages: [
      navigationPage('uniform', 'Uniform'),
      navigationPage('normal', 'Normal'),
      navigationPage('exponential', 'Exponential'),
      navigationPage('gamma', 'Gamma/Erlang'),
      navigationPage('student-t', 'Student t'),
      navigationPage('beta', 'Beta'),
      navigationPage('lognormal', 'Lognormal'),
      navigationPage('pareto', 'Pareto'),
      navigationPage('weibull', 'Weibull'),
      navigationPage('triangular', 'Triangular'),
    ],
  },
  {
    id: 'discrete', title: 'Discrete', pages: [
      navigationPage('bernoulli', 'Bernoulli'),
      navigationPage('poisson', 'Poisson'),
      navigationPage('weighted', 'Weighted'),
      navigationPage('empirical', 'Empirical'),
    ],
  },
] as const;

export const languageNavigation = [
  navigationPage('start'),
  navigationPage('syntax'),
  { id: 'randomness', title: 'Randomness', sections: randomnessSections },
  navigationPage('topology'),
  navigationPage('clients'),
  navigationPage('defaults'),
  navigationPage('tcp'),
  navigationPage('timeline'),
  navigationPage('examples'),
  navigationPage('errors'),
] as const;

export type LanguageNavigationGroupId = 'randomness' | typeof randomnessSections[number]['id'];

export function languageNavigationAncestors(pageId?: LanguagePageId): readonly LanguageNavigationGroupId[] {
  const section = randomnessSections.find(section => section.pages.some(page => page.id === pageId));
  return section ? ['randomness', section.id] : [];
}
