// Publish each chapter only after that family's language stage passes review.
// These loaders keep authored Markdown and the numerical preview out of App's initial chunk.
export const distributionPages = [
  {
    id: 'normal', title: 'Normal distributions', path: '/docs/language/normal',
    loadMarkdown: () => import('../../docs/language/normal.md?raw').then(module => module.default),
  },
] as const;
