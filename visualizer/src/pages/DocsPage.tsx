import {
  Alert, Box, Button, Collapse, List, ListItemButton, ListItemText, Stack, Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { lazy, memo, Suspense, useCallback, useEffect, useId, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { Link as RouterLink, useLocation, useNavigate } from 'react-router';
import readme from '../../../README.md?raw';
import DocumentationArticle from '../components/DocumentationArticle';
import { languageNavigation, languageNavigationAncestors, languagePages } from '../docs/language';
import { distributionPages } from '../docs/distributions';
import type { LanguageDocument, LanguageNavigationGroupId, LanguagePageId } from '../docs/language';
import { overviewPages, splitOverview } from '../docs/overview';
import type { OverviewDocument, OverviewPageId } from '../docs/overview';

const UniformDensityPreview = lazy(() => import('../components/UniformDensityPreview'));
const DistributionPreview = lazy(() => import('../components/DistributionPreview'));
const overviewContent = splitOverview(readme);
const navigationGroups = [
  { id: 'overview', title: 'Overview' },
  { id: 'language', title: 'Language documentation' },
] as const;
type NavigationGroup = typeof navigationGroups[number]['id'];

interface DocsPageProps {
  section?: OverviewPageId;
  languagePage?: LanguagePageId;
}

// Keep both documentation groups local and independent of backend availability.
function DocsPage({ section = 'introduction', languagePage }: DocsPageProps) {
  const { hash, key: locationKey } = useLocation();
  const navigate = useNavigate();
  const navigationId = useId();
  const activeGroup = languagePage ? 'language' : 'overview';
  const activePage = languagePage ?? section;
  const [expandedGroup, setExpandedGroup] = useState<NavigationGroup | null>(activeGroup);
  const [expandedLanguageGroups, setExpandedLanguageGroups] = useState(() => languageNavigationAncestors(languagePage));

  useEffect(() => {
    setExpandedGroup(activeGroup);
    setExpandedLanguageGroups(languageNavigationAncestors(languagePage));
  }, [activeGroup, languagePage, locationKey, hash]);

  function toggleLanguageGroup(id: LanguageNavigationGroupId) {
    setExpandedLanguageGroups(groups => groups.includes(id) ? groups.filter(group => group !== id) : [...groups, id]);
  }

  function navigationLink(page: { id: string; title: string; path: string }, group: NavigationGroup, depth = 1) {
    const selected = activeGroup === group && activePage === page.id;
    return (
      <ListItemButton
        key={page.path} component={RouterLink} to={page.path}
        selected={selected} aria-current={selected ? 'page' : undefined}
        sx={{ borderRadius: 1, pl: 2 + depth }}
      >
        <ListItemText primary={page.title} />
      </ListItemButton>
    );
  }

  useEffect(() => {
    if (languagePage) return;
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
    if (destination) navigate(destination, { replace: true });
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
        {navigationGroups.map(group => (
          <NavigationSection
            key={group.id} title={group.title} controlId={`${navigationId}-${group.id}`}
            expanded={expandedGroup === group.id}
            onToggle={() => setExpandedGroup(expandedGroup === group.id ? null : group.id)}
          >
            {group.id === 'overview' ? overviewPages.map(page => navigationLink(page, group.id)) : (
              languageNavigation.map(item => 'sections' in item ? (
                <NavigationSection
                  key={item.id} title={item.title} controlId={`${navigationId}-${item.id}`} depth={1}
                  expanded={expandedLanguageGroups.includes(item.id)} onToggle={() => toggleLanguageGroup(item.id)}
                >
                  {item.sections.map(section => (
                    <NavigationSection
                      key={section.id} title={section.title} controlId={`${navigationId}-${section.id}`} depth={2}
                      expanded={expandedLanguageGroups.includes(section.id)} onToggle={() => toggleLanguageGroup(section.id)}
                    >
                      {section.pages.map(page => navigationLink(page, 'language', 3))}
                    </NavigationSection>
                  ))}
                </NavigationSection>
              ) : navigationLink(item, 'language'))
            )}
          </NavigationSection>
        ))}
      </Box>
      {languagePage ? (
        <LanguageGuide key={languagePage} document={languagePages.find(page => page.id === languagePage)!} />
      ) : (
        <DocsOverview document={overviewContent.pages.find(page => page.id === section)!} />
      )}
    </Box>
  );
}

function NavigationSection({ title, controlId, expanded, onToggle, children, depth = 0 }: {
  title: string;
  controlId: string;
  expanded: boolean;
  onToggle: () => void;
  children: ReactNode;
  depth?: number;
}) {
  return (
    <Box>
      <ListItemButton
        aria-expanded={expanded} aria-controls={controlId} onClick={onToggle}
        sx={{ borderRadius: 1, pl: 2 + depth }}
      >
        <ListItemText primary={title} slotProps={{ primary: { fontWeight: 600 } }} />
        <ExpandMoreIcon sx={{ transform: expanded ? 'rotate(180deg)' : undefined }} />
      </ListItemButton>
      <Collapse in={expanded}>
        <List component="div" id={controlId} disablePadding>{children}</List>
      </Collapse>
    </Box>
  );
}

function LanguageGuide({ document }: { document: LanguageDocument }) {
  const { hash } = useLocation();
  const [markdown, setMarkdown] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [previewReady, setPreviewReady] = useState(false);
  const index = languagePages.indexOf(document);
  const previous = languagePages[index - 1];
  const next = languagePages[index + 1];

  // Loading the lazy chart can move headings below it; restore the bookmark once it mounts.
  const markPreviewReady = useCallback(() => setPreviewReady(true), []);
  const sectionContent = useMemo(() => {
    const preview = document.id === 'uniform'
      ? <UniformDensityPreview onReady={markPreviewReady} />
      : distributionPages.some(page => page.id === document.id)
        ? <DistributionPreview family={document.id} onReady={markPreviewReady} />
        : null;
    return preview ? {
      'density-preview': (
        <Suspense fallback={<Typography role="status">Loading density preview…</Typography>}>
          {preview}
        </Suspense>
      ),
    } : undefined;
  }, [document.id, markPreviewReady]);

  useEffect(() => {
    let current = true;
    setMarkdown(null);
    setFailed(false);
    document.loadMarkdown().then(
      source => { if (current) setMarkdown(source); },
      () => { if (current) setFailed(true); },
    );
    return () => { current = false; };
  }, [document, attempt]);

  useEffect(() => {
    if (markdown === null) return;
    if (!hash) {
      window.scrollTo(0, 0);
      return;
    }
    try {
      window.document.getElementById(decodeURIComponent(hash.slice(1)))?.scrollIntoView();
    } catch {
      // Ignore malformed bookmarks.
    }
  }, [markdown, hash, previewReady]);

  return (
    <Stack spacing={2} sx={{ minWidth: 0 }}>
      {markdown !== null ? (
        <DocumentationArticle
          title={document.title} markdown={markdown}
          sectionContent={sectionContent}
        />
      ) : failed ? (
        <Alert
          severity="error"
          action={<Button color="inherit" onClick={() => setAttempt(value => value + 1)}>Try again</Button>}
        >
          Unable to load {document.title}.
        </Alert>
      ) : (
        <Typography role="status" color="text.secondary">Loading {document.title}…</Typography>
      )}
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
