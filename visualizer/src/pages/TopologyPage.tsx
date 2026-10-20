import { Alert, Autocomplete, Box, Chip, createFilterOptions, Grid, Paper, Stack, TextField, Typography } from '@mui/material';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import { useCallback, useEffect, useMemo, useState } from 'react';
import type { SimulationMetadata } from '../types';
import TopologyCanvas from '../topology/TopologyCanvas';
import TopologyInspector from '../topology/TopologyInspector';
import { formatNumber, kindLabel, linkIsBusy } from '../topology/format';
import { visibleGraph } from '../topology/layout';
import type { TopologyGraph, TopologySelection } from '../topology/types';

interface TopologyPageProps {
  topology: TopologyGraph | null;
  simulation: SimulationMetadata | null;
  connected: boolean;
}

interface SearchOption {
  kind: 'node' | 'link';
  id: string;
  label: string;
  detail: string;
}

const searchOptions = createFilterOptions<SearchOption>({ limit: 50, stringify: (option) => `${option.label} ${option.detail} ${option.id}` });

function TopologyPage({ topology, simulation, connected }: TopologyPageProps) {
  const [selection, setSelection] = useState<TopologySelection>(null);
  const nodeById = useMemo(() => new Map(topology?.nodes.map((node) => [node.id, node]) ?? []), [topology?.nodes]);
  const linkById = useMemo(() => new Map(topology?.links.map((link) => [link.id, link]) ?? []), [topology?.links]);
  const nodeLabels = useMemo(() => new Map(topology?.nodes.map((node) => [node.id, node.label]) ?? []), [topology?.nodes]);
  const onSelect = useCallback((next: TopologySelection) => {
    setSelection((previous) => previous?.kind === next?.kind && previous?.id === next?.id ? previous : next);
  }, []);
  useEffect(() => {
    if (selection && !(selection.kind === 'node' ? nodeById : linkById).has(selection.id)) setSelection(null);
  }, [selection, nodeById, linkById]);

  const options = useMemo<SearchOption[]>(() => [
    ...(topology?.nodes.map((node) => ({ kind: 'node' as const, id: node.id, label: node.label, detail: `${kindLabel(node.kind)}${node.address ? ` · ${node.address}` : ''}` })) ?? []),
    ...(topology?.links.map((link) => ({ kind: 'link' as const, id: link.id, label: link.label, detail: `${nodeLabels.get(link.source) ?? link.source} → ${nodeLabels.get(link.target) ?? link.target}` })) ?? []),
  ], [topology, nodeLabels]);
  const selectedEntry = options.find((option) => option.kind === selection?.kind && option.id === selection.id);
  const selectedKind = selectedEntry?.kind;
  const selectedId = selectedEntry?.id;
  const selectedLabel = selectedEntry?.label;
  const selectedDetail = selectedEntry?.detail;
  // MUI resets an active search when its selected value changes identity. Keep
  // this value stable across metric-only snapshots, even as options refresh.
  const selectedOption = useMemo<SearchOption | null>(() => (
    selectedKind !== undefined && selectedId !== undefined && selectedLabel !== undefined && selectedDetail !== undefined
      ? { kind: selectedKind, id: selectedId, label: selectedLabel, detail: selectedDetail } : null
  ), [selectedKind, selectedId, selectedLabel, selectedDetail]);
  const graph = useMemo(() => topology ? visibleGraph(topology, selection) : null, [topology, selection]);
  const running = simulation?.status === 'running';
  const frozen = simulation?.status === 'finished' || simulation?.status === 'failed';
  const busyLinks = topology?.links.filter(linkIsBusy).length ?? 0;
  const empty = !topology || topology.nodes.length === 0;

  return (
    <Stack spacing={2.5} sx={{ py: 3 }}>
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} justifyContent="space-between" alignItems={{ sm: 'center' }}>
        <Box>
          <Typography variant="h5" component="h1" fontWeight={700}>Network topology</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>Explore the current experiment. Select a node or a directed link to see its settings and counters.</Typography>
        </Box>
        {simulation && <Chip label={`${simulation.status} · ${formatNumber(simulation.virtualTimeSeconds, 3)} s virtual`} variant="outlined" color={running ? 'primary' : simulation.status === 'failed' ? 'error' : 'default'} />}
      </Stack>

      {!connected && <Alert severity="warning">Disconnected. Any topology shown is the last received snapshot and may be out of date.</Alert>}
      {frozen && !empty && <Alert severity="info">This run has stopped advancing. Queued, transmitting or travelling packets show unfinished work in the final snapshot.</Alert>}

      {empty ? (
        <Paper variant="outlined" sx={{ py: 8, px: 3, textAlign: 'center' }}>
          <AccountTreeOutlinedIcon sx={{ fontSize: 48, color: 'text.disabled', mb: 1.5 }} />
          <Typography variant="h6">No topology to display</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
            {simulation?.status === 'idle' ? 'Start a simulation from the dashboard to build its topology.' : 'Waiting for a topology snapshot from the server.'}
          </Typography>
        </Paper>
      ) : graph && topology && (
        <>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Chip size="small" label={`${formatNumber(topology.nodes.length)} nodes`} />
            <Chip size="small" label={`${formatNumber(topology.links.length)} directed links`} />
            <Chip size="small" label={`${formatNumber(busyLinks)} ${frozen ? 'links with unfinished packets' : 'links with packets'}`} color={busyLinks > 0 ? 'info' : 'default'} variant="outlined" />
          </Stack>
          <Autocomplete
            options={options}
            value={selectedOption}
            onChange={(_, option) => onSelect(option ? { kind: option.kind, id: option.id } : null)}
            getOptionLabel={(option) => option.label}
            getOptionKey={(option) => JSON.stringify([option.kind, option.id])}
            isOptionEqualToValue={(option, value) => option.id === value.id && option.kind === value.kind}
            groupBy={(option) => option.kind === 'node' ? 'Nodes' : 'Directed links'}
            filterOptions={searchOptions}
            renderOption={(props, option) => {
              const { key, ...optionProps } = props;
              return <Box component="li" key={key} {...optionProps}><Box><Typography variant="body2">{option.label}</Typography><Typography variant="caption" color="text.secondary">{option.detail}</Typography></Box></Box>;
            }}
            renderInput={(params) => <TextField {...params} label="Find a node or directed link" size="small" helperText="Search labels, addresses or IDs. Up to 50 matches are shown; all graph elements are searchable." />}
          />
          {(graph.nodes.length < topology.nodes.length || graph.links.length < topology.links.length) && (
            <Alert severity="info">Showing {formatNumber(graph.nodes.length)} of {formatNumber(topology.nodes.length)} nodes and {formatNumber(graph.links.length)} of {formatNumber(topology.links.length)} links to keep the view responsive. Search selects any element and brings it into the graph.</Alert>
          )}
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, lg: 8 }}>
              <Paper variant="outlined" sx={{ overflow: 'hidden' }}><TopologyCanvas graph={graph} selection={selection} onSelect={onSelect} /></Paper>
              <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
                Pan to move · Scroll to zoom · Use the controls to fit the view · Press Enter to inspect a focused node or link
              </Typography>
              {topology.nodes.some((node) => node.kind === 'clientGroup') && <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>Client groups aggregate simulated clients. Each arrow represents one directed link reported by the model.</Typography>}
            </Grid>
            <Grid size={{ xs: 12, lg: 4 }}>
              <TopologyInspector node={selection?.kind === 'node' ? nodeById.get(selection.id) ?? null : null} link={selection?.kind === 'link' ? linkById.get(selection.id) ?? null : null} nodeLabels={nodeLabels} running={running && connected} />
            </Grid>
          </Grid>
        </>
      )}
    </Stack>
  );
}

export default TopologyPage;
