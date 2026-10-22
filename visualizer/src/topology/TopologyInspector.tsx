import {
  Box,
  Chip,
  Divider,
  LinearProgress,
  Paper,
  Stack,
  Typography,
} from '@mui/material';
import { useMemo } from 'react';
import {
  formatBandwidth,
  formatMetric,
  formatNumber,
  kindLabel,
  linkIsBusy,
} from './format';
import { describeLink } from './telemetry';
import { Fact, TrafficTotals } from './TopologyFacts';
import { BundleDetails, GroupDetails } from './TopologyGroupDetails';
import TopologyIngressDetails from './TopologyIngressDetails';
import type { TopologyBundle, TopologyGroup } from './grouping';
import type {
  TopologyGraph,
  TopologyLink,
  TopologyNode,
  TopologySelection,
} from './types';

interface TopologyInspectorProps {
  node: TopologyNode | null;
  link: TopologyLink | null;
  nodeLabels: Map<string, string>;
  running: boolean;
  group?: TopologyGroup | null;
  bundle?: TopologyBundle | null;
  onToggleGroup?: (groupId: string) => void;
  onSelect?: (selection: TopologySelection) => void;
  topology?: TopologyGraph | null;
}

function NodeDetails({ node, topology }: { node: TopologyNode; topology?: TopologyGraph | null }) {
  const hasActiveStatus = node.active !== undefined;
  const statusLabel = node.active ? 'Active' : 'Inactive';
  const statusColor = node.active ? 'success' : 'default';
  const ingress = node.kind === 'loadBalancer' ? topology?.ingress : undefined;
  const metrics = ingress
    ? node.metrics.filter((metric) => !metric.key.startsWith('ingress'))
    : node.metrics;
  const hasMetrics = metrics.length > 0;

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

      {ingress && (
        <>
          <Typography variant="subtitle2">Shared RX and input queues</Typography>
          <TopologyIngressDetails ingress={ingress} />
          <Divider />
        </>
      )}

      <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
        {node.resourceId && <Fact label="Resource ID" value={node.resourceId} />}
        {node.groupId && <Fact label="Client group" value={node.groupId} />}
        {node.address && <Fact label="Address" value={node.address} />}
        {metrics.map((metric) => (
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
  nodeById: ReadonlyMap<string, TopologyNode>;
  running: boolean;
}

function LinkDetails({ link, nodeLabels, nodeById, running }: LinkDetailsProps) {
  const { config, stats } = link;
  const sourceLabel = nodeLabels.get(link.source) ?? link.source;
  const targetLabel = nodeLabels.get(link.target) ?? link.target;
  const busy = linkIsBusy(link);
  const statusColor = busy ? 'info' : 'default';
  const role = describeLink(link, nodeById);

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

      <Typography variant="subtitle2">{role.label}</Typography>
      <Typography variant="body2" color="text.secondary">{role.explanation}</Typography>
      <Stack component="dl" sx={{ m: 0 }}>
        <Fact label="Waiting queue owner" value={role.owner} />
      </Stack>

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
      <Typography variant="caption" color="text.secondary">
        Transmission time is wire bytes × 8 ÷ bandwidth. Packet travel time is
        extra delay after sending; the next packet can be sent while the previous
        one travels.
      </Typography>

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

      {config.queueCapacity > 0 && (
        <LinearProgress
          variant="determinate"
          value={queuePercent}
          aria-label="Waiting queue occupancy"
        />
      )}
      <Typography variant="caption" color="text.secondary">
        Queue capacity excludes the packet being sent and packets already
        travelling. Zero waiting slots admit a packet only when transmission
        can start immediately.
      </Typography>

      <Divider />
      <Typography variant="subtitle2">Totals for this run</Typography>
      <TrafficTotals stats={stats} />
      <Typography variant="caption" color="text.secondary">
        Tail drops reject new packets when the waiting queue is full. Random
        losses happen after serialization and consume link bandwidth.
        Accepted packets may still be waiting, transmitting or travelling;
        delivered packets reached this link’s receiver. Byte totals include modeled headers.
      </Typography>
    </>
  );
}

function TopologyInspector({
  node,
  link,
  nodeLabels,
  running,
  group,
  bundle,
  onToggleGroup,
  onSelect,
  topology,
}: TopologyInspectorProps) {
  const emptySelection = !node && !link && !group && !bundle;
  const nodeById = useMemo(
    () => new Map(topology?.nodes.map((entry) => [entry.id, entry])),
    [topology],
  );

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

        {group ? (
          <GroupDetails
            group={group}
            topology={topology}
            onToggleGroup={onToggleGroup}
            onSelect={onSelect}
          />
        ) : node && <NodeDetails node={node} topology={topology} />}
        {bundle ? (
          <BundleDetails
            bundle={bundle}
            nodeById={nodeById}
            nodeLabels={nodeLabels}
            onSelect={onSelect}
          />
        ) : link && (
          <LinkDetails
            link={link}
            nodeLabels={nodeLabels}
            nodeById={nodeById}
            running={running}
          />
        )}
      </Stack>
    </Paper>
  );
}

export default TopologyInspector;
