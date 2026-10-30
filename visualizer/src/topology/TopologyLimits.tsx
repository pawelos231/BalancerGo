import { Box, Divider, Paper, Stack, Typography } from '@mui/material';
import { formatBandwidth, formatBytes, formatMetric, formatNumber } from './format';
import { linkValueRange, scopeLinks, totalLinks } from './telemetry';
import TopologyIngressDetails from './TopologyIngressDetails';
import type { TopologyGraph, TopologyLink, TopologyNode } from './types';

interface TopologyLimitsProps {
  topology: TopologyGraph;
}

interface CounterProps {
  label: string;
  value: string;
}

function Counter({ label, value }: CounterProps) {
  return (
    <Stack direction="row" justifyContent="space-between" spacing={2}>
      <Typography variant="caption" color="text.secondary">{label}</Typography>
      <Typography variant="caption" textAlign="right" fontWeight={600}>
        {value}
      </Typography>
    </Stack>
  );
}

interface ScopeProps {
  title: string;
  description: string;
  links: TopologyLink[];
  loadBalancers?: TopologyNode[];
}

// Counters stay within this set of hops; packets may appear again at the next hop.
function Scope({ title, description, links, loadBalancers }: ScopeProps) {
  const totals = totalLinks(links);
  const bandwidth = linkValueRange(
    links,
    (link) => link.config.bandwidthBitsPerSecond,
    formatBandwidth,
  );
  const configuredLabel = totals.linkCount === 1 ? 'Link limit' : 'Per-link limits';

  return (
    <Paper variant="outlined" sx={{ p: 2, minWidth: 0 }}>
      <Stack spacing={1}>
        <Typography variant="subtitle1" fontWeight={600}>{title}</Typography>
        <Typography variant="body2" color="text.secondary">{description}</Typography>

        {loadBalancers?.map((node) => (
          <Stack key={node.id} spacing={0.5}>
            {loadBalancers.length > 1 && (
              <Typography variant="caption" fontWeight={600}>{node.label}</Typography>
            )}
            {node.metrics.filter((metric) => (
              metric.key === 'forwardingBitsPerSecond' || metric.key === 'forwardingBudgetWaitMs'
            )).map((metric) => (
              <Counter key={metric.key} label={metric.label} value={formatMetric(metric)} />
            ))}
          </Stack>
        ))}
        {loadBalancers && loadBalancers.length > 0 && (
          <Typography variant="caption" color="text.secondary">
            Budget wait is virtual time with ready traffic held back by the shared limit.
          </Typography>
        )}

        <Counter label={configuredLabel} value={bandwidth} />
        <Counter label="Directed links" value={formatNumber(totals.linkCount)} />
        <Divider />
        <Counter label="Waiting now" value={`${formatNumber(totals.queuedPackets)} packets`} />
        <Counter
          label="Waiting slots across queues"
          value={`${formatNumber(totals.queueCapacity)} slots`}
        />
        <Counter label="Transmitting now" value={`${formatNumber(totals.transmittingLinks)} links`} />
        <Counter label="Travelling now" value={`${formatNumber(totals.travellingPackets)} packets`} />
        <Counter
          label="Delivered total"
          value={`${formatNumber(totals.deliveredPackets)} packets · ${formatBytes(totals.deliveredBytes)}`}
        />
        <Counter
          label="Tail drops total"
          value={`${formatNumber(totals.queueDroppedPackets)} packets · ${formatBytes(totals.queueDroppedBytes)}`}
        />
        <Counter
          label="Random losses total"
          value={`${formatNumber(totals.lostPackets)} packets · ${formatBytes(totals.lostBytes)}`}
        />
      </Stack>
    </Paper>
  );
}

// Display separate resource limits instead of adding access rates into throughput.
function TopologyLimits({ topology }: TopologyLimitsProps) {
  const scopes = scopeLinks(topology);
  const loadBalancers = topology.nodes.filter((node) => node.kind === 'loadBalancer');

  return (
    <Stack spacing={1.5}>
      <Box>
        <Typography variant="h6">Limits and packet counters</Typography>
        <Typography variant="body2" color="text.secondary">
          Bitrates are configured maxima. Waiting and activity describe this snapshot;
          delivery, drops and losses are totals per physical hop, including modeled headers.
        </Typography>
      </Box>
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', lg: 'repeat(3, minmax(0, 1fr))' },
          gap: 2,
        }}
      >
        <Scope
          title="Incoming access links"
          description={topology.ingress
            ? 'Each client and backend reaches an LB input queue through its own access link. These independent link limits apply before shared RX.'
            : 'Each endpoint has its own incoming access link and FIFO. The limits apply independently.'}
          links={scopes.access}
        />
        {topology.ingress ? (
          <Paper variant="outlined" sx={{ p: 2, minWidth: 0 }}>
            <Stack spacing={1.25}>
              <Typography variant="subtitle1" fontWeight={600}>Shared LB receiver</Typography>
              <Typography variant="body2" color="text.secondary">
                Frontside and backside use separate input FIFOs and one common RX limit.
              </Typography>
              <TopologyIngressDetails ingress={topology.ingress} />
            </Stack>
          </Paper>
        ) : (
          <Scope
            title="Shared LB ingress"
            description="Incoming paths join the shared physical LB input."
            links={scopes.ingress}
          />
        )}
        <Scope
          title="LB outputs and shared budget"
          description="Each outgoing path has its own link and FIFO. All LB outputs also compete for their owner's forwarding limit."
          links={scopes.egress}
          loadBalancers={loadBalancers}
        />
        {scopes.other.length > 0 && (
          <Scope
            title="Other directed links"
            description="These links retain their own configurations and counters. Inspect an arrow to see its endpoints."
            links={scopes.other}
          />
        )}
      </Box>
      <Typography variant="caption" color="text.secondary">
        Each card covers a separate transmission stage. A packet can cross several
        stages, so their delivery totals count hop traversals. Waiting slots stay separate.
      </Typography>
      <Typography variant="caption" color="text.secondary">
        Model: sliding-window TCP limited by the receive window, with fixed retry timers.
        Congestion control and kernel RX processing are not modeled; these queue drops
        do not predict real TCP behavior.
      </Typography>
    </Stack>
  );
}

export default TopologyLimits;
