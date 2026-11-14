import { Box, useTheme } from '@mui/material';
import { useEffect, useMemo, useRef } from 'react';
import { Compartment, EditorState } from '@codemirror/state';
import { basicSetup, EditorView } from 'codemirror';
import type { DocumentationExampleProps } from './DocumentationExample';
import { scenarioEditorAppearance } from './scenarioEditorAppearance';
import { scenarioFieldFragment, scenarioLanguage } from './scenarioLanguageHighlighting';

// Examples share editor colors and tokenization, without diagnostics or compilation.
export default function ReadOnlyScenarioExample({ source, fieldsOnly }: DocumentationExampleProps) {
  const container = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const appearance = useMemo(() => new Compartment(), []);
  const theme = useTheme();

  useEffect(() => {
    if (!container.current) return;
    const editor = new EditorView({
      parent: container.current,
      doc: source,
      extensions: [
        basicSetup,
        fieldsOnly ? scenarioFieldFragment : scenarioLanguage,
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        EditorView.contentAttributes.of({
          'aria-label': 'Scenario example source', 'aria-readonly': 'true', tabindex: '0',
        }),
        appearance.of([]),
      ],
    });
    view.current = editor;
    return () => { editor.destroy(); view.current = null; };
  }, [source, fieldsOnly, appearance]);

  // Recolor the existing view without losing the reader's selection.
  useEffect(() => {
    view.current?.dispatch({ effects: appearance.reconfigure([
      scenarioEditorAppearance(theme),
      EditorView.theme({
        '.cm-scroller': { lineHeight: '20px' },
        '.cm-content': { padding: '4px 0' },
      }),
    ]) });
  }, [theme, appearance, source, fieldsOnly]);

  return <Box ref={container} sx={{ minWidth: 0 }} />;
}
