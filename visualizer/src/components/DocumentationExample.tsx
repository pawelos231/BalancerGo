import { Box } from '@mui/material';
import { useEffect, useRef, useState } from 'react';
import type { ComponentType } from 'react';

export interface DocumentationExampleProps {
  source: string;
  fieldsOnly: boolean;
}

// Keep distant examples as plain text until the reader approaches them.
export default function DocumentationExample(props: DocumentationExampleProps) {
  const container = useRef<HTMLDivElement>(null);
  const [Example, setExample] = useState<ComponentType<DocumentationExampleProps> | null>(null);
  const height = props.source.split('\n').length * 20 + 8;

  useEffect(() => {
    const element = container.current;
    if (!element) return;
    let current = true;
    const loadEditor = () => {
      import('./ReadOnlyScenarioExample').then(
        module => { if (current) setExample(() => module.default); },
        () => { /* Keep the readable source if the editor chunk cannot load. */ },
      );
    };

    if (typeof IntersectionObserver === 'undefined') {
      loadEditor();
      return () => { current = false; };
    }

    const observer = new IntersectionObserver(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return;
      loadEditor();
      observer.disconnect();
    }, { rootMargin: '200px 0px' });

    observer.observe(element);
    return () => { current = false; observer.disconnect(); };
  }, []);

  // Reserve the editor's line height so mounting it does not move section anchors.
  const placeholder = (
    <Box
      component="pre" tabIndex={0}
      sx={{
        '&&': {
          m: 0, p: '4px 12px', height, border: 0, borderRadius: 0,
          fontSize: '13px', lineHeight: '20px', overflow: 'auto',
        },
      }}
    >
      {props.source}
    </Box>
  );

  return (
    <Box
      ref={container} role="region" aria-label="Read-only scenario example"
      sx={{
        my: 2, minWidth: 0, border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden',
        '&:focus-within': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 },
      }}
    >
      {Example ? <Example {...props} /> : placeholder}
    </Box>
  );
}
