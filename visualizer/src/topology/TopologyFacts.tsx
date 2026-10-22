import { Stack, Typography } from '@mui/material';
import { formatBytes, formatNumber } from './format';
import type { TopologyLinkStats } from './types';

// FactProps names one value in a description list.
export interface FactProps {
  label: string;
  value: string;
}

// Fact keeps labels and values aligned in the inspector.
export function Fact({ label, value }: FactProps) {
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

// TrafficCounters contains totals shared by physical links and bundles.
export type TrafficCounters = Omit<
  TopologyLinkStats,
  'transmitting' | 'queuedPackets' | 'travellingPackets'
>;

// The same totals describe one physical link or a set of distinct member links.
export function TrafficTotals({ stats }: { stats: TrafficCounters }) {
  return (
    <Stack component="dl" spacing={1.25} sx={{ m: 0 }}>
      <Fact label="Offered packets" value={formatNumber(stats.offeredPackets)} />
      <Fact label="Offered bytes" value={formatBytes(stats.offeredBytes)} />
      <Fact label="Accepted packets" value={formatNumber(stats.acceptedPackets)} />
      <Fact label="Accepted bytes" value={formatBytes(stats.acceptedBytes)} />
      <Fact label="Tail drops" value={formatNumber(stats.queueDroppedPackets)} />
      <Fact label="Queue-dropped bytes" value={formatBytes(stats.queueDroppedBytes)} />
      <Fact label="Random losses" value={formatNumber(stats.lostPackets)} />
      <Fact label="Lost bytes" value={formatBytes(stats.lostBytes)} />
      <Fact label="Delivered packets" value={formatNumber(stats.deliveredPackets)} />
      <Fact label="Delivered bytes" value={formatBytes(stats.deliveredBytes)} />
    </Stack>
  );
}

