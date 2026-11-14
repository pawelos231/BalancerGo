import type { Theme } from '@mui/material/styles';
import type { Extension } from '@codemirror/state';
import { EditorView } from 'codemirror';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';

export function scenarioEditorAppearance(theme: Theme): Extension {
  const dark = theme.palette.mode === 'dark';
  return [
    EditorView.theme({
      '&': { color: theme.palette.text.primary, backgroundColor: theme.palette.background.paper },
      '.cm-scroller': { fontFamily: 'monospace', fontSize: '13px' },
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
  ];
}
