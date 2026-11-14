import { Box, Link, Stack, Typography } from '@mui/material';
import { memo, useEffect } from 'react';
import { useLocation } from 'react-router';
import Markdown from 'react-markdown';
import type { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeSlug from 'rehype-slug';
import readme from '../../../README.md?raw';

const repositoryFiles = 'https://github.com/pawelos231/BalancerGo/blob/main/';

const markdownComponents: Components = {
  a({ href, children, title }) {
    // README file links resolve from the repository root; section links stay here.
    const destination = href && !href.startsWith('#')
      ? new URL(href, repositoryFiles).href : href;
    const external = destination && !destination.startsWith('#');

    return (
      <Link
        href={destination} title={title}
        target={external ? '_blank' : undefined}
        rel={external ? 'noopener noreferrer' : undefined}
      >
        {children}
      </Link>
    );
  },
  table({ children }) {
    return (
      <Box sx={{ overflowX: 'auto', my: 2 }}>
        <table>{children}</table>
      </Box>
    );
  },
};

// The README is bundled with the page, so reading docs does not need the backend.
function DocsPage() {
  const { hash } = useLocation();

  useEffect(() => {
    if (!hash) {
      window.scrollTo(0, 0);
      return;
    }

    // Wait until Markdown has rendered before following a section's anchor.
    let section: string;
    try {
      section = decodeURIComponent(hash.slice(1));
    } catch {
      return;
    }
    document.getElementById(section)?.scrollIntoView();
  }, [hash]);

  return (
    <Stack
      component="main" aria-label="Documentation" spacing={2}
      sx={{ py: 3, maxWidth: 1160, mx: 'auto', minWidth: 0 }}
    >
      <Box>
        <Typography variant="overline" color="text.secondary">Documentation</Typography>
        <Typography variant="body2" color="text.secondary">
          Project README — setup, architecture and configuration reference.
        </Typography>
      </Box>
      <Box
        component="article" aria-label="Project README"
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
          '& h2': {
            fontSize: '1.65rem', pb: 1, borderBottom: 1, borderColor: 'divider',
          },
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
          '& blockquote': {
            ml: 0, pl: 2, borderLeft: 3, borderColor: 'divider', color: 'text.secondary',
          },
          '& img': { maxWidth: '100%', height: 'auto' },
          '& hr': { border: 0, borderTop: 1, borderColor: 'divider', my: 3 },
        })}
      >
        <Markdown
          remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSlug]}
          components={markdownComponents} skipHtml
        >
          {readme}
        </Markdown>
      </Box>
    </Stack>
  );
}

// Live snapshots can refresh the surrounding app without reparsing this static guide.
export default memo(DocsPage);
