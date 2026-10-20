import {
  Box,
  Chip,
  Divider,
  LinearProgress,
  Paper,
  Stack,
  Typography,
} from '@mui/material';
import {
  formatBandwidth,
  formatBytes,
  formatMetric,
  formatNumber,
  kindLabel,
  linkIsBusy,
} from './format';
import type { TopologyLink, TopologyNode } from './types';

interface TopologyInspectorProps {
  node: TopologyNode | null;
  link: TopologyLink | null;
  nodeLabels: Map<string, string>;
  running: boolean;
}

interface FactProps {
  label: string;
  value: string;
}

function Fact({ label, value }: FactProps) {
  return (
    <Stack
      direction="row"
      justifyContent="space-between"
      alignItems="baseline"
      spacing={2}
    >
      <Typography component="dt" variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Typography
        component="dd"
        variant="body2"
        fontWeight={600}
        sx={{
          m: 0,
          textAlign: 'right',
          overflowWrap: 'anywhere',
        }}
      >
        {value}
      </Typography>
    </Stack>
  );
}

function NodeDetails({ node }: { node: TopologyNode }) {
  const hasActiveStatus = node.active !== undefined;
  const statusLabel = node.active ? 'Active' : 'Inactive';
  const statusColor = node.active ? 'success' : 'default';
  const hasMetrics = node.metrics.length > 0;

  return (
    <>
      <Box>
        <Typography variant="h6">{node.label}</Typography>
        <Typography variant="body2" color="text.secondary">
          {kindLabel(node.kind)}
        </Typography>
      </Box>

      {hasActiveStatus && (
        <Chip
          size="small"
          label={statusLabel}
          color={statusColor}
          sx={{ alignSelf: 'flex-start' }}
        />
      )}

      {node.description && (
        <Typography variant="body2" color="text.secondary">
          {node.description}
        </Typography>
      )}

      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        {node.address && <Fact label="Address" value={node.address} />}
        {node.metrics.map((metric) => (
          <Fact
            key={metric.key}
            label={metric.label}
            value={formatMetric(metric)}
          />
        ))}
      </Stack>

      {!hasMetrics && (
        <Typography variant="body2" color="text.secondary">
          No metrics were supplied for this node.
        </Typography>
      )}
    </>
  );
}

interface LinkDetailsProps {
  link: TopologyLink;
  nodeLabels: Map<string, string>;
  running: boolean;
}

function LinkDetails({ link, nodeLabels, running }: LinkDetailsProps) {
  const { config, stats } = link;
  const sourceLabel = nodeLabels.get(link.source) ?? link.source;
  const targetLabel = nodeLabels.get(link.target) ?? link.target;
  const busy = linkIsBusy(link);
  const statusColor = busy ? 'info' : 'default';

  let statusLabel = 'Idle link';
  if (busy) {
    statusLabel = running ? 'Packets in progress' : 'Unfinished packets';
  }

  let queuePercent = 0;
  if (config.queueCapacity > 0) {
    const queueOccupancy = stats.queuedPackets / config.queueCapacity;
    queuePercent = Math.min(100, queueOccupancy * 100);
  }

  return (
    <>
      <Box>
        <Typography variant="h6">{link.label}</Typography>
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ overflowWrap: 'anywhere' }}
        >
          {sourceLabel} → {targetLabel}
        </Typography>
      </Box>

      <Chip
        size="small"
        label={statusLabel}
        color={statusColor}
        sx={{ alignSelf: 'flex-start' }}
      />

      <Typography variant="subtitle2">Link settings</Typography>
      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        <Fact
          label="Bandwidth"
          value={formatBandwidth(config.bandwidthBitsPerSecond)}
        />
        <Fact
          label="Packet travel time"
          value={`${formatNumber(config.packetTravelTimeMs, 6)} ms`}
        />
        <Fact
          label="Queue capacity"
          value={`${formatNumber(config.queueCapacity)} packets`}
        />
        <Fact
          label="Loss probability"
          value={`${formatNumber(config.lossRate * 100, 4)}%`}
        />
      </Stack>

      <Divider />
      <Typography variant="subtitle2">Current activity</Typography>
      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        <Fact label="Waiting in queue" value={formatNumber(stats.queuedPackets)} />
        <Fact
          label="Transmitter"
          value={stats.transmitting ? 'Occupied' : 'Idle'}
        />
        <Fact
          label="Packets travelling"
          value={formatNumber(stats.travellingPackets)}
        />
      </Stack>

      <LinearProgress
        variant="determinate"
        value={queuePercent}
        aria-label="Waiting queue occupancy"
      />
      <Typography variant="caption" color="text.secondary">
        Queue capacity excludes the packet being sent and packets already
        travelling.
      </Typography>

      <Divider />
      <Typography variant="subtitle2">Totals for this run</Typography>
      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        <Fact label="Offered packets" value={formatNumber(stats.offeredPackets)} />
        <Fact
          label="Delivered packets"
          value={formatNumber(stats.deliveredPackets)}
        />
        <Fact
          label="Delivered wire bytes"
          value={formatBytes(stats.deliveredBytes)}
        />
        <Fact
          label="Queue drops (total)"
          value={formatNumber(stats.queueDroppedPackets)}
        />
        <Fact
          label="Link losses (total)"
          value={formatNumber(stats.lostPackets)}
        />
      </Stack>
      <Typography variant="caption" color="text.secondary">
        Delivered packets and bytes count arrival at the end of this link.
        Wire bytes include modeled protocol headers.
      </Typography>
    </>
  );
}

function TopologyInspector({
  node,
  link,
  nodeLabels,
  running,
}: TopologyInspectorProps) {
  const emptySelection = !node && !link;

  return (
    <Paper
      variant="outlined"
      sx={{
        p: 2.5,
        height: '100%',
        boxSizing: 'border-box',
        minHeight: 260,
      }}
    >
      <Stack spacing={2}>
        <Typography variant="overline" color="text.secondary">
          Topology inspector
        </Typography>

        {emptySelection && (
          <>
            <Typography variant="h6">Explore the topology</Typography>
            <Typography variant="body2" color="text.secondary">
              Select a node or an arrow. You can also search for any node or
              directed link above the graph.
            </Typography>

            <Divider />
            <Typography variant="body2" color="text.secondary">
              Opposite arrows are independent links. Queue and packet activity
              describe the current snapshot; drop counters are totals for this run.
            </Typography>
          </>
        )}

        {node && <NodeDetails node={node} />}
        {link && (
          <LinkDetails
            link={link}
            nodeLabels={nodeLabels}
            running={running}
          />
        )}
      </Stack>
    </Paper>
  );
}

export default TopologyInspector;
