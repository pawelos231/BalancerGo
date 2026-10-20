import {
  Alert,
  Autocomplete,
  Box,
  Chip,
  createFilterOptions,
  Grid,
  Paper,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import { useMemo } from 'react';
import type { SimulationMetadata } from '../types';
import TopologyCanvas from '../topology/TopologyCanvas';
import TopologyInspector from '../topology/TopologyInspector';
import { formatNumber, linkIsBusy } from '../topology/format';
import { visibleGraph } from '../topology/layout';
import type { TopologyGraph } from '../topology/types';
import useTopologySelection from './useTopologySelection';
import type { TopologySearchOption } from './useTopologySelection';

interface TopologyPageProps {
  topology: TopologyGraph | null;
  simulation: SimulationMetadata | null;
  connected: boolean;
}

const filterSearchOptions = createFilterOptions<TopologySearchOption>({
  limit: 50,
  stringify: (option) => `${option.label} ${option.detail} ${option.id}`,
});

function TopologyPage({ topology, simulation, connected }: TopologyPageProps) {
  const {
    selection,
    onSelect,
    options,
    selectedOption,
    selectedNode,
    selectedLink,
    nodeLabels,
  } = useTopologySelection(topology);

  const graph = useMemo(
    () => topology ? visibleGraph(topology, selection) : null,
    [topology, selection],
  );

  const status = simulation?.status;
  const running = status === 'running';
  const failed = status === 'failed';
  const frozen = status === 'finished' || failed;
  const statusColor = running ? 'primary' : failed ? 'error' : 'default';
  const statusLabel = simulation
    ? `${status} · ${formatNumber(simulation.virtualTimeSeconds, 3)} s virtual`
    : '';

  const totalNodes = topology?.nodes.length ?? 0;
  const totalLinks = topology?.links.length ?? 0;
  const visibleNodes = graph?.nodes.length ?? 0;
  const visibleLinks = graph?.links.length ?? 0;
  const empty = totalNodes === 0;
  const limitedView = visibleNodes < totalNodes || visibleLinks < totalLinks;
  const hasClientGroup = topology?.nodes.some((node) => node.kind === 'clientGroup');

  const busyLinks = topology?.links.filter(linkIsBusy).length ?? 0;
  const busyLinkDescription = frozen
    ? 'links with unfinished packets'
    : 'links with packets';
  const busyLinkLabel = `${formatNumber(busyLinks)} ${busyLinkDescription}`;
  const busyLinkColor = busyLinks > 0 ? 'info' : 'default';

  const emptyMessage = status === 'idle'
    ? 'Start a simulation from the dashboard to build its topology.'
    : 'Waiting for a topology snapshot from the server.';

  return (
    <Stack spacing={2.5} sx={{ py: 3 }}>
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        spacing={2}
        justifyContent="space-between"
        alignItems={{ sm: 'center' }}
      >
        <Box>
          <Typography variant="h5" component="h1" fontWeight={700}>
            Network topology
          </Typography>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ mt: 0.5 }}
          >
            Explore the current experiment. Select a node or a directed link to see
            its settings and counters.
          </Typography>
        </Box>

        {simulation && (
          <Chip
            label={statusLabel}
            variant="outlined"
            color={statusColor}
          />
        )}
      </Stack>

      {!connected && (
        <Alert severity="warning">
          Disconnected. Any topology shown is the last received snapshot and may
          be out of date.
        </Alert>
      )}

      {frozen && !empty && (
        <Alert severity="info">
          This run has stopped advancing. Queued, transmitting or travelling
          packets show unfinished work in the final snapshot.
        </Alert>
      )}

      {empty ? (
        <Paper
          variant="outlined"
          sx={{
            py: 8,
            px: 3,
            textAlign: 'center',
          }}
        >
          <AccountTreeOutlinedIcon
            sx={{
              fontSize: 48,
              color: 'text.disabled',
              mb: 1.5,
            }}
          />
          <Typography variant="h6">No topology to display</Typography>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ mt: 1 }}
          >
            {emptyMessage}
          </Typography>
        </Paper>
      ) : graph && topology && (
        <>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Chip size="small" label={`${formatNumber(totalNodes)} nodes`} />
            <Chip
              size="small"
              label={`${formatNumber(totalLinks)} directed links`}
            />
            <Chip
              size="small"
              label={busyLinkLabel}
              color={busyLinkColor}
              variant="outlined"
            />
          </Stack>

          <Autocomplete
            options={options}
            value={selectedOption}
            onChange={(_, option) => {
              const next = option ? { kind: option.kind, id: option.id } : null;
              onSelect(next);
            }}
            getOptionLabel={(option) => option.label}
            getOptionKey={(option) => JSON.stringify([option.kind, option.id])}
            isOptionEqualToValue={(option, value) => (
              option.id === value.id && option.kind === value.kind
            )}
            groupBy={(option) => (
              option.kind === 'node' ? 'Nodes' : 'Directed links'
            )}
            filterOptions={filterSearchOptions}
            renderOption={(props, option) => {
              const { key, ...optionProps } = props;

              return (
                <Box component="li" key={key} {...optionProps}>
                  <Box>
                    <Typography variant="body2">{option.label}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {option.detail}
                    </Typography>
                  </Box>
                </Box>
              );
            }}
            renderInput={(params) => (
              <TextField
                {...params}
                label="Find a node or directed link"
                size="small"
                helperText="Search labels, addresses or IDs. Up to 50 matches are shown; all graph elements are searchable."
              />
            )}
          />

          {limitedView && (
            <Alert severity="info">
              Showing {formatNumber(visibleNodes)} of {formatNumber(totalNodes)}
              {' '}nodes and {formatNumber(visibleLinks)} of {formatNumber(totalLinks)}
              {' '}links to keep the view responsive. Search selects any element
              and brings it into the graph.
            </Alert>
          )}

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, lg: 8 }}>
              <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
                <TopologyCanvas
                  graph={graph}
                  selection={selection}
                  onSelect={onSelect}
                />
              </Paper>

              <Typography
                variant="caption"
                color="text.secondary"
                display="block"
                sx={{ mt: 1 }}
              >
                Pan to move · Scroll to zoom · Use the controls to fit the view ·
                Press Enter to inspect a focused node or link
              </Typography>

              {hasClientGroup && (
                <Typography
                  variant="caption"
                  color="text.secondary"
                  display="block"
                  sx={{ mt: 0.5 }}
                >
                  Client groups aggregate simulated clients. Each arrow
                  represents one directed link reported by the model.
                </Typography>
              )}
            </Grid>

            <Grid size={{ xs: 12, lg: 4 }}>
              <TopologyInspector
                node={selectedNode}
                link={selectedLink}
                nodeLabels={nodeLabels}
                running={running && connected}
              />
            </Grid>
          </Grid>
        </>
      )}
    </Stack>
  );
}

export default TopologyPage;
