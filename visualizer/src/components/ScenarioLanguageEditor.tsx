import { Alert, Box, Button, Chip, Stack, Typography, useTheme } from '@mui/material';
import { useEffect, useMemo, useRef } from 'react';
import { basicSetup, EditorView } from 'codemirror';
import { Compartment, EditorState } from '@codemirror/state';
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import { setDiagnostics } from '@codemirror/lint';
import type { LanguageError, SourcePoint } from '../scenario-language/source';

interface ScenarioLanguageEditorProps {
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

const keywords = new Set([
  'language', 'run', 'defaults', 'tcp', 'balancer', 'backend', 'backends',
  'clients', 'client', 'profile', 'use', 'at', 'set', 'add', 'down', 'up', 'drain', 'stop', 'arrivals',
]);

// Highlighting is permissive; the existing compiler remains the only semantic validator.
const language = StreamLanguage.define({
  token(stream) {
    if (stream.eatSpace()) return null;
    if (stream.eat('#')) {
      stream.skipToEnd();
      return 'comment';
    }
    if (stream.eat('"')) {
      let escaped = false;
      while (!stream.eol()) {
        const character = stream.next();
        if (character === '"' && !escaped) break;
        escaped = character === '\\' && !escaped;
      }
      return 'string';
    }
    if (stream.match(/^-?[0-9]+(?:\.[0-9]+)?(?:[A-Za-z]+|%)?/)) return 'number';
    if (stream.match(/^[A-Za-z_][A-Za-z0-9_-]*/)) {
      return keywords.has(stream.current()) ? 'keyword' : 'variableName';
    }
    stream.next();
    return null;
  },
});

export default function ScenarioLanguageEditor({
  source, dirty, disabled, applied, diagnostic, onApply, onImport, onExport, onChange, onDiscard,
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

  useEffect(() => { change.current = onChange; }, [onChange]);

  // Create one editor; later prop/theme updates keep selection, undo and scroll intact.
  useEffect(() => {
    if (!container.current) return;
    const editor = new EditorView({
      parent: container.current,
      doc: initialSource.current,
      extensions: [
        basicSetup, language, readonly.of([]), appearance.of([]),
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
    const errors = diagnostic ? [{
      from: position(diagnostic.range.start),
      to: position(diagnostic.range.end),
      severity: 'error' as const,
      message: diagnostic.message,
    }] : [];
    editor.dispatch(setDiagnostics(editor.state, errors));
  }, [diagnostic]);

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
        { tag: tags.keyword, color: theme.palette.primary.main },
        { tag: tags.string, color: dark ? theme.palette.success.light : theme.palette.success.dark },
        { tag: tags.number, color: dark ? theme.palette.warning.light : theme.palette.warning.dark },
        { tag: tags.comment, color: theme.palette.text.secondary },
      ])),
    ]) });
  }, [theme, appearance]);

  return (
    <Stack spacing={1.5} sx={{ minWidth: 0 }}>
      <Typography variant="body2" color="text.secondary">
        Link units are bytes/s: 1GB means 8 Gbit/s. Compile and apply validates the
        generated JSON locally and on the server before replacing the applied draft.
        It never starts a simulation.
      </Typography>
      <Chip
        label={dirty ? 'Unapplied source edits' : applied ? 'Source applied' : 'Source draft'}
        color={applied ? 'success' : 'default'} size="small" sx={{ alignSelf: 'flex-start' }}
      />
      <Box ref={container} sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }} />
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button variant="contained" disabled={disabled || (applied && !dirty)} onClick={onApply}>
          Compile and apply
        </Button>
        <Button disabled={disabled || !dirty} onClick={onDiscard}>Discard source edits</Button>
      </Stack>
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
      {diagnostic && <Alert severity="error" sx={{ overflowWrap: 'anywhere' }}>{diagnostic.message}</Alert>}
      {!applied && !dirty && (
        <Alert severity="info">
          This source does not describe the applied JSON. Compile and apply it to replace that configuration.
        </Alert>
      )}
      {dirty && <Alert severity="warning">Compile and apply or discard the source edits before using this draft.</Alert>}
    </Stack>
  );
}
