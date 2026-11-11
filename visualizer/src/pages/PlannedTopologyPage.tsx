import {
  Alert, Box, Button, Chip, Grid, Paper, Slider, Stack, Typography,
} from '@mui/material';
import { useMemo, useState } from 'react';
import { Link as RouterLink } from 'react-router';
import {
  formatVirtualSeconds, plannedStateTimes, projectPlannedTopology,
} from '../scenarios/plannedTopology';
import { projectPlannedTopologyGraph } from '../scenarios/plannedTopologyGraph';
import type { Scenario } from '../scenarios/types';
import PlannedResourceDetails, { PlannedLinkDetails } from '../topology/PlannedResourceDetails';
import TopologyCanvas from '../topology/TopologyCanvas';
import TopologySearch from '../topology/TopologySearch';
import { Fact } from '../topology/TopologyFacts';
import { visibleGraph } from '../topology/layout';
import type { TopologySearchOption, TopologySelection } from '../topology/types';

interface PlannedTopologyPageProps {
  scenario: Scenario | null;
  error: string | null;
}

// The shared drawing shows configured resources, without inventing live measurements.
function PlannedTopologyPage({ scenario, error }: PlannedTopologyPageProps) {
  return (
    <Stack spacing={2.5} sx={{ py: 3 }}>
      <Box>
        <Typography variant="h5" component="h1" fontWeight={700}>
          Planned topology
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          Inspect the applied scenario before Start. Move the slider to a configured
          timestamp, then select a node or a directed link for its settings.
        </Typography>
      </Box>
      <Alert severity="info">
        This graph shows declared configuration. Client budgets are not active connections;
        clients already created may survive a later budget decrease or group removal.
        Traffic and observed health are available in Live topology after Start.
      </Alert>
      {error && (
        <Alert severity="warning" sx={{ overflowWrap: 'anywhere' }}>
          {scenario ? 'Showing the last valid applied scenario. ' : 'No valid scenario to preview. '}
          {error}
          {' '}
          <Button component={RouterLink} to="/" color="inherit" size="small">
            Open JSON editor
          </Button>
        </Alert>
      )}
      {scenario ? <PlannedTimeline scenario={scenario} /> : (
        <Alert severity="info">Waiting for valid scenario defaults or an imported scenario.</Alert>
      )}
    </Stack>
  );
}

function PlannedTimeline({ scenario }: { scenario: Scenario }) {
  const [requestedAt, setRequestedAt] = useState('0');
  const [selection, setSelection] = useState<TopologySelection>(null);
  const times = useMemo(() => plannedStateTimes(scenario), [scenario]);

  // If an edited document removes the chosen timestamp, use its previous state.
  let selectedIndex = 0;
  for (let index = 0; index < times.length; index++) {
    if (BigInt(times[index]) > BigInt(requestedAt)) break;
    selectedIndex = index;
  }
  const selectedAt = times[selectedIndex];
  const plan = useMemo(
    () => projectPlannedTopology(scenario, BigInt(selectedAt)),
    [scenario, selectedAt],
  );
  const diagram = useMemo(() => projectPlannedTopologyGraph(plan), [plan]);
  const { graph } = diagram;
  const selectedNode = selection?.kind === 'node'
    ? graph.nodes.find(node => node.id === selection.id)
    : undefined;
  const selectedLink = selection?.kind === 'link'
    ? diagram.linkDetails.get(selection.id)
    : undefined;
  const currentSelection = selectedNode || selectedLink ? selection : null;
  const drawing = visibleGraph(graph, currentSelection);
  const resource = selectedNode ? diagram.nodeResources.get(selectedNode.id) : undefined;
  const limited = drawing.nodes.length < graph.nodes.length || drawing.links.length < graph.links.length;

  const options = useMemo<TopologySearchOption[]>(() => [
    ...graph.nodes.map(node => ({
      kind: 'node' as const,
      id: node.id,
      label: node.label,
      detail: [node.description, node.address, node.resourceId, node.id].filter(Boolean).join(' · '),
    })),
    ...graph.links.map(link => ({
      kind: 'link' as const,
      id: link.id,
      label: link.label,
      detail: `${link.source} → ${link.target}`,
    })),
  ], [graph]);
  const selectedOption = options.find(option => (
    option.kind === currentSelection?.kind && option.id === currentSelection.id
  )) ?? null;
  const marks = times.map((time, index) => ({
    value: index,
    label: times.length <= 7 || index === 0 || index === times.length - 1 || index === selectedIndex
      ? `${formatVirtualSeconds(time)} s`
      : undefined,
  }));

  return (
    <>
      <Paper variant="outlined" sx={{ p: 2 }}>
        <Stack spacing={2}>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Chip label="Planned configuration" color="info" size="small" />
            <Chip label={`${times.length} declared states`} size="small" />
          </Stack>
          <Typography id="planned-timestamp-label" variant="subtitle2">
            Configured timestamp
          </Typography>
          <Box sx={{ px: 2, pb: 1 }}>
            <Slider
              aria-labelledby="planned-timestamp-label"
              min={0}
              max={Math.max(1, times.length - 1)}
              marks={marks}
              step={1}
              value={selectedIndex}
              disabled={times.length === 1}
              onChange={(_, value) => {
                const index = Array.isArray(value) ? value[0] : value;
                setRequestedAt(times[index]);
              }}
              valueLabelDisplay="auto"
              valueLabelFormat={index => `${formatVirtualSeconds(times[index] ?? selectedAt)} s`}
              getAriaValueText={index => {
                const time = times[index] ?? selectedAt;
                return `${formatVirtualSeconds(time)} seconds (${time} nanoseconds)`;
              }}
              sx={{
                '& .MuiSlider-markLabel[data-index="0"]': { transform: 'none' },
                [`& .MuiSlider-markLabel[data-index="${times.length - 1}"]`]: {
                  transform: times.length === 1 ? 'none' : 'translateX(-100%)',
                },
              }}
            />
          </Box>
          <Typography variant="body2" role="status">
            Preview at {formatVirtualSeconds(selectedAt)} s ({selectedAt} ns).
          </Typography>
          <Typography variant="caption" color="text.secondary">
            Each slider position selects one timestamp from the configuration.
            Changing the preview never starts or advances a simulation.
          </Typography>
        </Stack>
      </Paper>

      <TopologySearch
        options={options}
        value={selectedOption}
        onSelect={setSelection}
        label="Find a configured group, slot, backend or directed link"
        helperText="Search labels, addresses and IDs. Select a hidden resource to bring it into view. Up to 50 matches are listed."
      />
      {limited && (
        <Alert severity="info">
          Showing {drawing.nodes.length} of {graph.nodes.length} configured nodes.
          Search for a hidden resource or link to bring it into view.
        </Alert>
      )}

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 8 }}>
          <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
            <TopologyCanvas
              graph={drawing}
              selection={currentSelection}
              onSelect={setSelection}
              viewKey={`planned:${selectedAt}`}
              mode="planned"
            />
          </Paper>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
            Pan to move · Scroll to zoom · Fit the view with the controls · Select a node or link
          </Typography>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>
            Client groups stay compact. Each represented slot has independent directional links;
            all paths share the LB ingress limit and forwarding budget.
          </Typography>
        </Grid>
        <Grid size={{ xs: 12, lg: 4 }}>
          <Paper variant="outlined" sx={{ p: 2, overflowWrap: 'anywhere' }}>
            {selectedLink ? (
              <Stack spacing={2}>
                <Typography variant="h6" component="h2">Directed link</Typography>
                <Stack component="dl" spacing={1} sx={{ m: 0 }}>
                  <Fact label="Independent links" value={String(selectedLink.memberCount)} />
                </Stack>
                <PlannedLinkDetails title={selectedLink.label} link={selectedLink.config} />
                <Typography variant="caption" color="text.secondary">
                  These are configured limits per link. Queue occupancy and traffic require a simulation.
                </Typography>
              </Stack>
            ) : (
              <PlannedResourceDetails
                plan={plan}
                selection={resource?.resource ?? 'loadBalancer'}
                slotIndex={resource?.slotIndex}
              />
            )}
          </Paper>
        </Grid>
      </Grid>
    </>
  );
}

export default PlannedTopologyPage;
