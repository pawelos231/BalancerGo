import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useMemo, useState } from 'react';

interface ScenarioDocumentPreviewProps {
  title: string;
  document: object;
  description: string;
}

// Large documents are formatted only when opened and reused between live snapshots.
function ScenarioDocumentPreview({ title, document, description }: ScenarioDocumentPreviewProps) {
  const [expanded, setExpanded] = useState(false);
  const text = useMemo(() => expanded ? JSON.stringify(document, null, 2) : '', [document, expanded]);

  return (
    <Accordion
      variant="outlined"
      disableGutters
      expanded={expanded}
      onChange={(_event, nextExpanded) => setExpanded(nextExpanded)}
    >
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Typography variant="body2">{title}</Typography>
      </AccordionSummary>
      <AccordionDetails>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          {description}
        </Typography>
        {expanded && (
          <Box component="pre" sx={{ m: 0, maxHeight: 360, overflow: 'auto', fontSize: 12 }}>
            {text}
          </Box>
        )}
      </AccordionDetails>
    </Accordion>
  );
}

export default ScenarioDocumentPreview;
