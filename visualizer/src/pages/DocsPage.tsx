import {
  Alert, Box, Button, Collapse, List, ListItemButton, ListItemText, Stack, Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { memo, useEffect, useState } from 'react';
import { Link as RouterLink, useLocation, useNavigate } from 'react-router';
import readme from '../../../README.md?raw';
import DocumentationArticle from '../components/DocumentationArticle';
import { languagePages } from '../docs/language';
import type { LanguageDocument, LanguagePageId } from '../docs/language';
import { overviewPages, splitOverview } from '../docs/overview';
import type { OverviewDocument, OverviewPageId } from '../docs/overview';

const overviewContent = splitOverview(readme);
const navigationGroups = [
  { id: 'overview', title: 'Overview', pages: overviewPages },
  { id: 'language', title: 'Language documentation', pages: languagePages },
] as const;
type NavigationGroup = typeof navigationGroups[number]['id'];

interface DocsPageProps {
  section?: OverviewPageId;
  languagePage?: LanguagePageId;
}

// Keep both documentation groups local and independent of backend availability.
function DocsPage({ section = 'introduction', languagePage }: DocsPageProps) {
  const { hash } = useLocation();
  const navigate = useNavigate();
  const activeGroup = languagePage ? 'language' : 'overview';
  const activePage = languagePage ?? section;
  const [expandedGroup, setExpandedGroup] = useState<NavigationGroup | null>(activeGroup);

  useEffect(() => { setExpandedGroup(activeGroup); }, [activeGroup]);

  useEffect(() => {
    if (!hash) {
      window.scrollTo(0, 0);
      return;
    }
    let anchorId: string;
    try {
      anchorId = decodeURIComponent(hash.slice(1));
    } catch {
      return;
    }
    const heading = document.getElementById(anchorId);
    if (heading) {
      heading.scrollIntoView();
      return;
    }

    // Old README bookmarks can now belong to another Overview page.
    const destination = overviewContent.anchorLinks.get(anchorId);
    if (!languagePage && destination) navigate(destination, { replace: true });
  }, [hash, section, languagePage, navigate]);

  return (
    <Box
      component="main" aria-label="Documentation"
      sx={{
        display: 'grid', gap: 3, py: 3, maxWidth: 1480, mx: 'auto', minWidth: 0,
        gridTemplateColumns: { xs: 'minmax(0, 1fr)', md: '240px minmax(0, 1fr)' },
        alignItems: 'start',
      }}
    >
      <Box
        component="nav" aria-label="Documentation sections"
        sx={{
          position: { xs: 'static', md: 'sticky' }, top: 24, alignSelf: 'start',
          minWidth: 0, pr: { xs: 0, md: 2 }, pb: { xs: 2, md: 0 },
          maxHeight: { xs: 320, md: 'calc(100dvh - 48px)' }, overflowY: 'auto',
          borderRight: { xs: 0, md: 1 }, borderBottom: { xs: 1, md: 0 }, borderColor: 'divider',
        }}
      >
        <Typography variant="overline" color="text.secondary" sx={{ px: 2 }}>Documentation</Typography>
        {navigationGroups.map(group => {
          const expanded = expandedGroup === group.id;
          return (
            <Box key={group.id}>
              <ListItemButton
                aria-expanded={expanded} aria-controls={`${group.id}-navigation`}
                onClick={() => setExpandedGroup(expanded ? null : group.id)}
                sx={{ borderRadius: 1 }}
              >
                <ListItemText primary={group.title} slotProps={{ primary: { fontWeight: 600 } }} />
                <ExpandMoreIcon sx={{ transform: expanded ? 'rotate(180deg)' : undefined }} />
              </ListItemButton>
              <Collapse in={expanded}>
                <List component="div" id={`${group.id}-navigation`} disablePadding>
                  {group.pages.map(item => {
                    const selected = activeGroup === group.id && activePage === item.id;
                    return (
                      <ListItemButton
                        key={item.path} component={RouterLink} to={item.path}
                        selected={selected} aria-current={selected ? 'page' : undefined}
                        sx={{ borderRadius: 1, pl: 3 }}
                      >
                        <ListItemText primary={item.title} />
                      </ListItemButton>
                    );
                  })}
                </List>
              </Collapse>
            </Box>
          );
        })}
      </Box>
      {languagePage ? (
        <LanguageGuide document={languagePages.find(page => page.id === languagePage)!} />
      ) : (
        <DocsOverview document={overviewContent.pages.find(page => page.id === section)!} />
      )}
    </Box>
  );
}

function LanguageGuide({ document }: { document: LanguageDocument }) {
  const index = languagePages.indexOf(document);
  const previous = languagePages[index - 1];
  const next = languagePages[index + 1];

  return (
    <Stack spacing={2} sx={{ minWidth: 0 }}>
      <DocumentationArticle title={document.title} markdown={document.markdown} />
      <Stack direction="row" spacing={1} justifyContent="space-between">
        {previous && <Button component={RouterLink} to={previous.path}>← {previous.title}</Button>}
        {next && <Button component={RouterLink} to={next.path} sx={{ ml: 'auto' }}>{next.title} →</Button>}
      </Stack>
    </Stack>
  );
}

function DocsOverview({ document }: { document: OverviewDocument }) {
  return (
    <Stack spacing={2} sx={{ minWidth: 0 }}>
      <Alert severity="warning" variant="filled" sx={{ fontWeight: 800, fontSize: '1rem' }}>
        PLACEHOLDER DOCS GENERATED FULLY BY AI, WILL BE CHANGED
      </Alert>
      <Box>
        <Typography variant="overline" color="text.secondary">Overview</Typography>
        <Typography variant="h4" component={document.id === 'introduction' ? 'p' : 'h1'}>
          {document.title}
        </Typography>
      </Box>
      <DocumentationArticle title={document.title} markdown={document.markdown} overviewPath={document.path} />
    </Stack>
  );
}

// Live snapshots do not need to reparse the static guides.
export default memo(DocsPage);
