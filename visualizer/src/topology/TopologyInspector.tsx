import { Box, Chip, Divider, LinearProgress, Paper, Stack, Typography } from '@mui/material';
import { formatBandwidth, formatBytes, formatMetric, formatNumber, kindLabel, linkIsBusy } from './format';
import type { TopologyLink, TopologyNode } from './types';

interface TopologyInspectorProps {
  node: TopologyNode | null;
  link: TopologyLink | null;
  nodeLabels: Map<string, string>;
  running: boolean;
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <Stack direction="row" justifyContent="space-between" alignItems="baseline" spacing={2}>
      <Typography component="dt" variant="body2" color="text.secondary">{label}</Typography>
      <Typography component="dd" variant="body2" fontWeight={600} sx={{ m: 0, textAlign: 'right', overflowWrap: 'anywhere' }}>{value}</Typography>
    </Stack>
  );
}

function TopologyInspector({ node, link, nodeLabels, running }: TopologyInspectorProps) {
  const queuePercent = link && link.config.queueCapacity > 0 ? Math.min(100, link.stats.queuedPackets / link.config.queueCapacity * 100) : 0;
  return (
    <Paper variant="outlined" sx={{ p: 2.5, height: '100%', boxSizing: 'border-box', minHeight: 260 }}>
      <Stack spacing={2}>
        <Typography variant="overline" color="text.secondary">Topology inspector</Typography>
        {!node && !link && (
          <>
            <Typography variant="h6">Explore the topology</Typography>
            <Typography variant="body2" color="text.secondary">Select a node or an arrow. You can also search for any node or directed link above the graph.</Typography>
            <Divider />
            <Typography variant="body2" color="text.secondary">Opposite arrows are independent links. Queue and packet activity describe the current snapshot; drop counters are totals for this run.</Typography>
          </>
        )}
        {node && (
          <>
            <Box><Typography variant="h6">{node.label}</Typography><Typography variant="body2" color="text.secondary">{kindLabel(node.kind)}</Typography></Box>
            {node.active !== undefined && <Chip size="small" label={node.active ? 'Active' : 'Inactive'} color={node.active ? 'success' : 'default'} sx={{ alignSelf: 'flex-start' }} />}
            {node.description && <Typography variant="body2" color="text.secondary">{node.description}</Typography>}
            <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
              {node.address && <Fact label="Address" value={node.address} />}
              {node.metrics.map((metric) => <Fact key={metric.key} label={metric.label} value={formatMetric(metric)} />)}
            </Stack>
            {node.metrics.length === 0 && <Typography variant="body2" color="text.secondary">No metrics were supplied for this node.</Typography>}
          </>
        )}
        {link && (
          <>
            <Box><Typography variant="h6">{link.label}</Typography><Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>{nodeLabels.get(link.source) ?? link.source} → {nodeLabels.get(link.target) ?? link.target}</Typography></Box>
            <Chip size="small" label={linkIsBusy(link) ? running ? 'Packets in progress' : 'Unfinished packets' : 'Idle link'} color={linkIsBusy(link) ? 'info' : 'default'} sx={{ alignSelf: 'flex-start' }} />
            <Typography variant="subtitle2">Link settings</Typography>
            <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
              <Fact label="Bandwidth" value={formatBandwidth(link.config.bandwidthBitsPerSecond)} />
              <Fact label="Packet travel time" value={`${formatNumber(link.config.packetTravelTimeMs, 6)} ms`} />
              <Fact label="Queue capacity" value={`${formatNumber(link.config.queueCapacity)} packets`} />
              <Fact label="Loss probability" value={`${formatNumber(link.config.lossRate * 100, 4)}%`} />
            </Stack>
            <Divider />
            <Typography variant="subtitle2">Current activity</Typography>
            <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
              <Fact label="Waiting in queue" value={formatNumber(link.stats.queuedPackets)} />
              <Fact label="Transmitter" value={link.stats.transmitting ? 'Occupied' : 'Idle'} />
              <Fact label="Packets travelling" value={formatNumber(link.stats.travellingPackets)} />
            </Stack>
            <LinearProgress variant="determinate" value={queuePercent} aria-label="Waiting queue occupancy" />
            <Typography variant="caption" color="text.secondary">Queue capacity excludes the packet being sent and packets already travelling.</Typography>
            <Divider />
            <Typography variant="subtitle2">Totals for this run</Typography>
            <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
              <Fact label="Offered packets" value={formatNumber(link.stats.offeredPackets)} />
              <Fact label="Delivered packets" value={formatNumber(link.stats.deliveredPackets)} />
              <Fact label="Delivered wire bytes" value={formatBytes(link.stats.deliveredBytes)} />
              <Fact label="Queue drops (total)" value={formatNumber(link.stats.queueDroppedPackets)} />
              <Fact label="Link losses (total)" value={formatNumber(link.stats.lostPackets)} />
            </Stack>
            <Typography variant="caption" color="text.secondary">Delivered packets and bytes count arrival at the end of this link. Wire bytes include modeled protocol headers.</Typography>
          </>
        )}
      </Stack>
    </Paper>
  );
}

export default TopologyInspector;
