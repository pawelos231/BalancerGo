import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Chip,
  Stack,
  Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useState } from 'react';
import type { CurrentSimulation } from '../scenarios/types';

interface ScenarioRunDetailsProps {
  run: CurrentSimulation;
}

// Read the exact server document after reload, including profiles and group overrides.
function ScenarioRunDetails({ run }: ScenarioRunDetailsProps) {
  const [expanded, setExpanded] = useState(false);
  const { scenario } = run;
  const groups = scenario.workload.clientGroups;
  const visibleGroups = groups.slice(0, 12);

  return (
    <Stack spacing={1}>
      <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
        <Typography variant="subtitle2">Current run: {run.runId}</Typography>
        <Chip label={run.status} size="small" />
      </Stack>

      <Typography variant="body2" color="text.secondary">
        {scenario.resources.backends.length} backends · {groups.length} client groups
        {' · '}Algorithm: {scenario.resources.loadBalancer.algorithm}
      </Typography>

      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        {visibleGroups.map((group) => (
          <Chip
            key={group.id}
            label={`${group.id}: ${group.count} scheduled clients`}
            size="small"
            variant="outlined"
          />
        ))}
      </Stack>
      {groups.length > visibleGroups.length && (
        <Typography variant="caption" color="text.secondary">
          Remaining groups are listed in the complete scenario below.
        </Typography>
      )}

      <Accordion
        variant="outlined"
        disableGutters
        expanded={expanded}
        onChange={(_event, nextExpanded) => setExpanded(nextExpanded)}
      >
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography variant="body2">Exact run scenario</Typography>
        </AccordionSummary>
        <AccordionDetails>
          {expanded && (
            <Box
              component="pre"
              sx={{
                m: 0,
                maxHeight: 360,
                overflow: 'auto',
                fontSize: 12,
                whiteSpace: 'pre',
              }}
            >
              {JSON.stringify(scenario, null, 2)}
            </Box>
          )}
        </AccordionDetails>
      </Accordion>
      <Typography variant="caption" color="text.secondary">
        This is the scenario accepted at start. Runtime algorithm changes and added clients
        appear in the live state; they do not rewrite this document.
      </Typography>
    </Stack>
  );
}

export default ScenarioRunDetails;
