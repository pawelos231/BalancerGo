import {
  Alert,
  Box,
  Chip,
  Grid,
  Paper,
  Stack,
  Typography,
} from '@mui/material';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import RemoveOutlinedIcon from '@mui/icons-material/RemoveOutlined';
import type { SimulationMetadata } from '../types';
import TopologyCanvas from '../topology/TopologyCanvas';
import TopologyInspector from '../topology/TopologyInspector';
import TopologyLimits from '../topology/TopologyLimits';
import TopologySearch from '../topology/TopologySearch';
import { formatNumber, linkIsBusy } from '../topology/format';
import type { TopologyGraph } from '../topology/types';
import useTopologyView from './useTopologyView';

interface TopologyPageProps {
  topology: TopologyGraph | null;
  simulation: SimulationMetadata | null;
  connected: boolean;
}

// Coordinate the grouped drawing, physical search and live inspector.
function TopologyPage({ topology, simulation, connected }: TopologyPageProps) {
  const {
    selection,
    onSelect,
    options,
    selectedOption,
    selectedNode,
    selectedLink,
    nodeLabels,
    graph,
    grouped,
    selectedGroup,
    selectedBundle,
    toggleGroup,
    viewKey,
  } = useTopologyView(topology);

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
  const drawnNodes = grouped?.graph.nodes.length ?? 0;
  const drawnLinks = grouped?.graph.links.length ?? 0;
  const empty = totalNodes === 0;
  const limitedView = visibleNodes < drawnNodes || visibleLinks < drawnLinks;

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
      ) : graph && topology && grouped && (
        <>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Chip size="small" label={`${formatNumber(totalNodes)} physical nodes`} />
            <Chip
              size="small"
              label={`${formatNumber(totalLinks)} independent directed links`}
            />
            <Chip
              size="small"
              label={busyLinkLabel}
              color={busyLinkColor}
              variant="outlined"
            />
          </Stack>

          {grouped.groups.length > 0 && (
            <Stack spacing={1}>
              <Typography variant="subtitle2">Client groups</Typography>
              <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                {grouped.groups.map((group) => {
                  const action = group.expanded ? 'Collapse' : 'Expand';
                  const count = formatNumber(group.clients.length);

                  return (
                    <Chip
                      key={group.id}
                      label={`${action} ${group.label} (${count})`}
                      icon={group.expanded ? <RemoveOutlinedIcon /> : <AddOutlinedIcon />}
                      variant={group.expanded ? 'filled' : 'outlined'}
                      onClick={() => toggleGroup(group.id)}
                      aria-label={`${action} group ${group.label}`}
                    />
                  );
                })}
              </Stack>
              <Typography variant="caption" color="text.secondary">
                A label such as 100 × 100 Mbit/s means 100 separate links.
                Their traffic still shares one LB ingress and one forwarding budget.
                Search can show a single client while the rest stay grouped.
              </Typography>
            </Stack>
          )}

          <TopologySearch
            options={options}
            value={selectedOption}
            onSelect={onSelect}
          />

          {limitedView && (
            <Alert severity="info">
              Showing {formatNumber(visibleNodes)} of {formatNumber(drawnNodes)}
              {' '}display nodes and {formatNumber(visibleLinks)} of {formatNumber(drawnLinks)}
              {' '}display arrows to keep the view responsive.
              Collapse a group for an overview, or search for a specific resource.
            </Alert>
          )}

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, lg: 8 }}>
              <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
                <TopologyCanvas
                  graph={graph}
                  selection={selection}
                  onSelect={onSelect}
                  viewKey={viewKey}
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

              <Typography
                variant="caption"
                color="text.secondary"
                display="block"
                sx={{ mt: 0.5 }}
              >
                Clients connect on the left and backends on the right.
                Both input queues share one RX limit; all outputs share one TX limit.
              </Typography>
            </Grid>

            <Grid size={{ xs: 12, lg: 4 }}>
              <TopologyInspector
                node={selectedNode}
                link={selectedLink}
                nodeLabels={nodeLabels}
                running={running && connected}
                topology={topology}
                group={selectedGroup}
                bundle={selectedBundle}
                onToggleGroup={toggleGroup}
                onSelect={onSelect}
              />
            </Grid>
          </Grid>

          <TopologyLimits topology={topology} />
        </>
      )}
    </Stack>
  );
}

export default TopologyPage;
