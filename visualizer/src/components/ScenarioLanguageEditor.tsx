import { Alert, Box, Button, Chip, IconButton, Stack, Tooltip, Typography, useTheme } from '@mui/material';
import FullscreenIcon from '@mui/icons-material/Fullscreen';
import FullscreenExitIcon from '@mui/icons-material/FullscreenExit';
import FocusTrap from '@mui/material/Unstable_TrapFocus';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useLocation } from 'react-router';
import { basicSetup, EditorView } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import { setDiagnostics } from '@codemirror/lint';
import type { LanguageError, SourcePoint } from '../scenario-language/source';
import useLanguageDiagnostics from '../hooks/useLanguageDiagnostics';
import DocumentationLink from './DocumentationLink';
import { scenarioLanguage } from './scenarioLanguageHighlighting';

interface ScenarioLanguageEditorProps {
  active: boolean;
  configuration: ReactNode;
  appliedDocument: string | null;
  source: string;
  dirty: boolean;
  disabled: boolean;
  applied: boolean;
  diagnostic: LanguageError | null;
  onApply: () => void;
  onImport: (file: File) => void;
  onExport: () => void;
  onChange: (source: string) => void;
  onDiscard: () => void;
}

export default function ScenarioLanguageEditor({
  active, configuration, appliedDocument, source, dirty, disabled, applied, diagnostic,
  onApply, onImport, onExport, onChange, onDiscard,
}: ScenarioLanguageEditorProps) {
  const container = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const view = useRef<EditorView | null>(null);
  const initialSource = useRef(source);
  const change = useRef(onChange);
  const syncing = useRef(false);
  const readonly = useMemo(() => new Compartment(), []);
  const appearance = useMemo(() => new Compartment(), []);
  const theme = useTheme();
  const { pathname } = useLocation();
  const live = useLanguageDiagnostics(source, active && pathname === '/');
  const visibleDiagnostic = diagnostic ?? live.diagnostic;
  const [expanded, setExpanded] = useState(false);
  const fullscreen = expanded && active && pathname === '/';
  const focusBeforeFullscreen = useRef<HTMLElement | null>(null);
  const fullscreenLabel = fullscreen ? 'Exit fullscreen (F7 or Esc)' : 'Enter fullscreen (F7)';

  const toggleFullscreen = useCallback(() => {
    if (!expanded) {
      const focused = document.activeElement;
      focusBeforeFullscreen.current = focused instanceof HTMLElement ? focused : null;
    }
    setExpanded(previous => !previous);
  }, [expanded]);

  // The hidden setup stays mounted; shortcuts belong only to the active source editor.
  useEffect(() => {
    if (!active || pathname !== '/') {
      setExpanded(false);
      return;
    }

    const handleShortcut = (event: KeyboardEvent) => {
      const modified = event.altKey || event.ctrlKey || event.metaKey || event.shiftKey;
      if (event.key === 'F7' && !modified) {
        event.preventDefault();
        if (!event.repeat) toggleFullscreen();
      } else if (event.key === 'Escape' && !event.defaultPrevented) {
        setExpanded(false);
      }
    };
    window.addEventListener('keydown', handleShortcut);
    return () => window.removeEventListener('keydown', handleShortcut);
  }, [active, pathname, toggleFullscreen]);

  useEffect(() => {
    if (!fullscreen) return;
    const previousOverflow = document.body.style.overflow;
    const previousFocus = focusBeforeFullscreen.current;
    document.body.style.overflow = 'hidden';

    return () => {
      document.body.style.overflow = previousOverflow;
      if (previousFocus?.isConnected && previousFocus.getClientRects().length > 0) {
        previousFocus.focus({ preventScroll: true });
      }
    };
  }, [fullscreen]);

  useEffect(() => { view.current?.requestMeasure(); }, [fullscreen]);

  useEffect(() => { change.current = onChange; }, [onChange]);

  // Create one editor; later prop/theme updates keep selection, undo and scroll intact.
  useEffect(() => {
    if (!container.current) return;
    const editor = new EditorView({
      parent: container.current,
      doc: initialSource.current,
      extensions: [
        basicSetup, scenarioLanguage, readonly.of([]), appearance.of([]),
        EditorView.contentAttributes.of({ 'aria-label': 'Scenario language source', spellcheck: 'false' }),
        EditorView.updateListener.of(update => {
          if (update.docChanged && !syncing.current) change.current(update.state.doc.toString());
        }),
      ],
    });
    view.current = editor;
    return () => { editor.destroy(); view.current = null; };
  }, [readonly, appearance]);

  // Parent-owned text survives navigation and explicit discard without creating input callbacks.
  useEffect(() => {
    const editor = view.current;
    if (!editor || editor.state.doc.toString() === source.replace(/\r\n?/g, '\n')) return;
    syncing.current = true;
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: source } });
    syncing.current = false;
  }, [source, disabled]);

  useEffect(() => {
    view.current?.dispatch({ effects: readonly.reconfigure([
      EditorState.readOnly.of(disabled), EditorView.editable.of(!disabled),
    ]) });
  }, [disabled, readonly]);

  // Map line/column to the displayed document: imported CRLF text has different raw offsets.
  useEffect(() => {
    const editor = view.current;
    if (!editor) return;
    const position = (point: SourcePoint) => {
      const line = editor.state.doc.line(Math.min(point.line, editor.state.doc.lines));
      return line.from + Math.min(point.column - 1, line.length);
    };
    const errors = visibleDiagnostic ? [{
      from: position(visibleDiagnostic.range.start),
      to: position(visibleDiagnostic.range.end),
      severity: 'error' as const,
      message: visibleDiagnostic.message,
    }] : [];
    editor.dispatch(setDiagnostics(editor.state, errors));
  }, [visibleDiagnostic]);

  useEffect(() => {
    const dark = theme.palette.mode === 'dark';
    view.current?.dispatch({ effects: appearance.reconfigure([
      EditorView.theme({
        '&': { color: theme.palette.text.primary, backgroundColor: theme.palette.background.paper },
        '.cm-scroller': { fontFamily: 'monospace', fontSize: '13px', minHeight: '280px', maxHeight: '480px' },
        '.cm-gutters': { color: theme.palette.text.secondary, backgroundColor: theme.palette.background.paper },
        '.cm-activeLine': { backgroundColor: theme.palette.action.hover },
        '.cm-cursor': { borderLeftColor: theme.palette.primary.main },
        '&.cm-focused': { outline: 'none' },
      }, { dark }),
      syntaxHighlighting(HighlightStyle.define([
        { tag: tags.keyword, color: dark ? theme.palette.primary.main : theme.palette.primary.dark },
        { tag: tags.variableName, color: dark ? '#CE93D8' : '#7B1FA2' },
        { tag: [tags.bool, tags.null], color: dark ? '#FFAB91' : '#BF360C' },
        { tag: tags.string, color: dark ? theme.palette.success.light : theme.palette.success.dark },
        { tag: tags.number, color: dark ? theme.palette.warning.light : '#955800' },
        { tag: tags.punctuation, color: theme.palette.text.secondary },
        { tag: tags.comment, color: theme.palette.text.secondary },
      ])),
    ]) });
  }, [theme, appearance]);

  // Change only the layout: CodeMirror keeps the same DOM, selection and undo history.
  const workspace = (
    <Box
      role={fullscreen ? 'dialog' : 'region'} aria-modal={fullscreen || undefined}
      aria-label="Scenario source editor" tabIndex={-1}
      sx={{
        display: 'grid',
        minWidth: 0,
        gap: fullscreen ? 0 : 1.5,
        ...(fullscreen && {
          position: 'fixed', inset: 0, zIndex: theme.zIndex.modal,
          bgcolor: 'background.paper', height: '100dvh',
          gridTemplateColumns: { xs: 'minmax(0, 1fr)', md: 'minmax(0, 1fr) min(560px, 40vw)' },
          gridTemplateRows: { xs: 'minmax(0, 3fr) minmax(0, 2fr)', md: 'minmax(0, 1fr)' },
        }),
      }}
    >
      <Box sx={{ minWidth: 0, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        <Stack
          direction="row" alignItems="center" justifyContent="space-between"
          sx={{ px: 1, py: 0.5, flexShrink: 0, border: 1, borderColor: 'divider' }}
        >
          <Typography variant="caption" color="text.secondary">Scenario language</Typography>
          <Stack direction="row" alignItems="center" spacing={0.5}>
            <DocumentationLink />
            <Tooltip title={fullscreenLabel} slotProps={{ popper: { disablePortal: true } }}>
              <IconButton
                size="small" aria-label={fullscreenLabel}
                aria-keyshortcuts="F7" onClick={toggleFullscreen}
              >
                {fullscreen ? <FullscreenExitIcon /> : <FullscreenIcon />}
              </IconButton>
            </Tooltip>
          </Stack>
        </Stack>
        <Box
          ref={container}
          sx={{
            border: 1, borderTop: 0, borderColor: 'divider', overflow: 'hidden',
            ...(fullscreen && {
              flex: 1, minHeight: 0,
              '& .cm-editor': { height: '100%' },
              '& .cm-scroller': { minHeight: 0, maxHeight: 'none', overflow: 'auto' },
            }),
          }}
        />
      </Box>
      <Stack
        component="aside" aria-label="Scenario configuration" spacing={1.5}
        sx={{ minWidth: 0, minHeight: 0, ...(fullscreen && { p: 2, overflow: 'hidden' }) }}
      >
        {!fullscreen && (
          <Chip
            label={dirty ? 'Unapplied source edits' : applied ? 'Source applied' : 'Source draft'}
            color={applied ? 'success' : 'default'} size="small" sx={{ alignSelf: 'flex-start' }}
          />
        )}
        <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap sx={{ flexShrink: 0 }}>
          <Button variant="contained" disabled={disabled || (applied && !dirty)} onClick={onApply}>
            Compile and apply
          </Button>
          {!fullscreen && (
            <Button disabled={disabled || !dirty} onClick={onDiscard}>Discard source edits</Button>
          )}
        </Stack>
        {fullscreen && (
          <Stack spacing={1} sx={{ flex: 1, minWidth: 0, minHeight: 0 }}>
            <Typography variant="subtitle2">Applied scenario JSON</Typography>
            {!applied && appliedDocument !== null && (
              <Typography variant="caption" color="text.secondary">
                Compile and apply to update this JSON from the source.
              </Typography>
            )}
            {appliedDocument === null ? (
              <Typography variant="body2" color="text.secondary">No scenario has been applied yet.</Typography>
            ) : (
              <Box
                component="pre" role="region" aria-label="Applied scenario JSON" tabIndex={0}
                sx={{
                  flex: 1, minWidth: 0, minHeight: 0, m: 0, p: 1.5,
                  overflow: 'auto', fontFamily: 'monospace', fontSize: 12, lineHeight: 1.5,
                  bgcolor: 'background.default', border: 1, borderColor: 'divider', borderRadius: 1,
                }}
              >
                {appliedDocument}
              </Box>
            )}
          </Stack>
        )}
        {/* Omit extra controls so keyboard focus cannot enter them in fullscreen. */}
        {!fullscreen && (
          <Stack spacing={1.5} sx={{ minWidth: 0 }}>
            <input
              ref={fileInput} type="file" accept=".lb,text/plain" aria-label="Scenario language file"
              disabled={disabled || dirty} hidden
              onChange={event => {
                const file = event.currentTarget.files?.[0];
                event.currentTarget.value = '';
                if (file) onImport(file);
              }}
            />
            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
              <Button disabled={disabled || dirty} onClick={() => fileInput.current?.click()}>Import source</Button>
              <Button disabled={disabled || source.trim() === ''} onClick={onExport}>Export source</Button>
            </Stack>
            <Typography variant="caption" color="text.secondary">
              Up to 2 MiB of UTF-8 source. Import loads a draft; Apply updates JSON.
              Export source includes comments and unapplied edits.
            </Typography>
            {visibleDiagnostic && (
              <Alert severity="error" sx={{ overflowWrap: 'anywhere' }}>{visibleDiagnostic.message}</Alert>
            )}
            {live.failure && <Alert severity="warning">{live.failure}</Alert>}
            {!applied && !dirty && (
              <Alert severity="info">
                This source does not describe the applied JSON. Compile and apply it to replace that configuration.
              </Alert>
            )}
            {dirty && <Alert severity="warning">Compile and apply or discard the source edits before using this draft.</Alert>}
            <Typography variant="body2" color="text.secondary">
              Link units are bytes/s: 1GB means 8 Gbit/s. Compile and apply validates the
              generated JSON locally and on the server before replacing the applied draft.
              It never starts a simulation.
            </Typography>
            {configuration}
          </Stack>
        )}
      </Stack>
    </Box>
  );

  return (
    <Box sx={{ minWidth: 0 }}>
      <FocusTrap open={fullscreen} disableRestoreFocus>{workspace}</FocusTrap>
    </Box>
  );
}
