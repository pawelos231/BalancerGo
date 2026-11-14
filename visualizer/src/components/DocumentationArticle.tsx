import { Box, Link } from '@mui/material';
import { Link as RouterLink } from 'react-router';
import Markdown from 'react-markdown';
import type { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeSlug from 'rehype-slug';
import { remarkConceptBacklinks, remarkConceptLinks } from '../docs/concepts';
import { languagePages } from '../docs/language';
import DocumentationExample from './DocumentationExample';

const repositoryFiles = 'https://github.com/pawelos231/BalancerGo/blob/main/';
const documentationSources = new Map<string, string>([
  ['visualizer/docs/scenario-language.md', '/docs/language'],
  ...languagePages.map(page => [`visualizer/docs/language/${page.id}.md`, page.path] as const),
]);

const markdownComponents: Components = {
  pre({ node, children }) {
    const code = node?.children[0];
    if (code?.type === 'element' && code.tagName === 'code') {
      const classes = code.properties.className;
      const scenario = Array.isArray(classes) && classes.some(name =>
        ['language-lb', 'language-lb-fragment', 'language-lb-invalid'].includes(String(name)));
      if (scenario) {
        const source = code.children.map(child => child.type === 'text' ? child.value : '').join('');
        const context = String(code.data?.meta ?? '').match(/\bcontext="?([a-z-]+)"?(?:\s|$)/)?.[1];
        const fieldsOnly = ['tcp', 'balancer', 'backend', 'clients'].includes(context ?? '');
        return <DocumentationExample source={source.replace(/\n$/, '')} fieldsOnly={fieldsOnly} />;
      }
    }
    return <pre>{children}</pre>;
  },
  a({ href, children, title }) {
    const destination = documentationSources.get(href ?? '') ?? href;
    if (destination && /^\/docs(?:\/|#|$)/.test(destination)) {
      return <Link component={RouterLink} to={destination} title={title}>{children}</Link>;
    }

    // Section links stay here; other README files resolve from the repository root.
    const external = destination && !destination.startsWith('#');
    return (
      <Link
        href={external ? new URL(destination, repositoryFiles).href : destination} title={title}
        target={external ? '_blank' : undefined} rel={external ? 'noopener noreferrer' : undefined}
      >
        {children}
      </Link>
    );
  },
  table({ children }) {
    return <Box sx={{ overflowX: 'auto', my: 2 }}><table>{children}</table></Box>;
  },
};

interface DocumentationArticleProps {
  title: string;
  markdown: string;
  overviewPath?: string;
}

export default function DocumentationArticle({ title, markdown, overviewPath }: DocumentationArticleProps) {
  return (
    <Box
      component="article" aria-label={title}
      sx={theme => ({
        p: { xs: 2, md: 4 }, minWidth: 0,
        bgcolor: 'background.paper', border: 1, borderColor: 'divider', borderRadius: 2,
        fontSize: 16, lineHeight: 1.75, overflowWrap: 'anywhere',
        '& > :first-child': { mt: 0 },
        '& > :last-child': { mb: 0 },
        '& h1, & h2, & h3, & h4, & h5, & h6': {
          lineHeight: 1.3, mt: 4, mb: 1.5, scrollMarginTop: 24,
        },
        '& h1': { fontSize: '2.25rem' },
        '& h2': { fontSize: '1.65rem', pb: 1, borderBottom: 1, borderColor: 'divider' },
        '& h3': { fontSize: '1.3rem' },
        '& p, & ul, & ol': { my: 1.5 },
        '& ul, & ol': { pl: 3 },
        '& li + li': { mt: 0.5 },
        '& a': {
          color: theme.palette.mode === 'dark' ? theme.palette.primary.light : theme.palette.primary.dark,
        },
        '& code': {
          fontFamily: 'monospace', fontSize: '0.875em',
          bgcolor: 'action.hover', px: 0.5, py: 0.25, borderRadius: 0.5,
        },
        '& pre': {
          p: 2, overflowX: 'auto', bgcolor: 'background.default',
          border: 1, borderColor: 'divider', borderRadius: 1, lineHeight: 1.6,
        },
        '& pre code': { bgcolor: 'transparent', p: 0 },
        '& table': { borderCollapse: 'collapse', width: '100%', fontSize: '0.9em' },
        '& th, & td': {
          border: 1, borderColor: 'divider', p: 1.5, textAlign: 'left', verticalAlign: 'top',
        },
        '& th': { bgcolor: 'action.hover' },
        '& blockquote': { ml: 0, pl: 2, borderLeft: 3, borderColor: 'divider', color: 'text.secondary' },
        '& img': { maxWidth: '100%', height: 'auto' },
        '& hr': { border: 0, borderTop: 1, borderColor: 'divider', my: 3 },
      })}
    >
      <Markdown
        remarkPlugins={[remarkGfm, remarkConceptLinks, [remarkConceptBacklinks, { path: overviewPath }]]}
        rehypePlugins={[rehypeSlug]} components={markdownComponents} skipHtml
      >
        {markdown}
      </Markdown>
    </Box>
  );
}
