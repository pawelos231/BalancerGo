// Publish each chapter only after that family's language stage passes review.
// These loaders keep authored Markdown and the numerical preview out of App's initial chunk.
export const distributionPages = [
  {
    id: 'normal', title: 'Normal distributions', path: '/docs/language/normal',
    loadMarkdown: () => import('../../docs/language/normal.md?raw').then(module => module.default),
  },
  {
    id: 'exponential', title: 'Exponential distributions', path: '/docs/language/exponential',
    loadMarkdown: () => import('../../docs/language/exponential.md?raw').then(module => module.default),
  },
  {
    id: 'gamma', title: 'Gamma distributions', path: '/docs/language/gamma',
    loadMarkdown: () => import('../../docs/language/gamma.md?raw').then(module => module.default),
  },
  {
    id: 'student-t', title: 'Student t distributions', path: '/docs/language/student-t',
    loadMarkdown: () => import('../../docs/language/student-t.md?raw').then(module => module.default),
  },
  {
    id: 'beta', title: 'Beta distributions', path: '/docs/language/beta',
    loadMarkdown: () => import('../../docs/language/beta.md?raw').then(module => module.default),
  },
  {
    id: 'lognormal', title: 'Lognormal distributions', path: '/docs/language/lognormal',
    loadMarkdown: () => import('../../docs/language/lognormal.md?raw').then(module => module.default),
  },
  {
    id: 'pareto', title: 'Pareto distributions', path: '/docs/language/pareto',
    loadMarkdown: () => import('../../docs/language/pareto.md?raw').then(module => module.default),
  },
] as const;
