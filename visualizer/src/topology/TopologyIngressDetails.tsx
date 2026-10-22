import { Box, Divider, LinearProgress, Stack, Typography } from '@mui/material';
import { formatBandwidth, formatBytes, formatNumber } from './format';
import { Fact } from './TopologyFacts';
import type { TopologyIngress } from './types';

// The shared receiver is one resource; its two waiting queues keep their own slots.
function TopologyIngressDetails({ ingress }: { ingress: TopologyIngress }) {
  const { config, stats } = ingress;
  const capacity = ingress.queues.reduce((sum, queue) => sum + queue.capacity, 0);
  const waiting = `${formatNumber(stats.queuedPackets)} / ${formatNumber(capacity)} packets`;
  const delivered = `${formatNumber(stats.deliveredPackets)} packets · ${formatBytes(stats.deliveredBytes)}`;
  const losses = `${formatNumber(stats.lostPackets)} packets · ${formatBytes(stats.lostBytes)}`;

  return (
    <Stack spacing={1.25}>
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Common RX limit" value={formatBandwidth(config.bandwidthBitsPerSecond)} />
        <Fact
          label="Travel after RX transmission"
          value={`${formatNumber(config.packetTravelTimeMs, 6)} ms`}
        />
        <Fact
          label="Loss after RX transmission"
          value={`${formatNumber(config.lossRate * 100, 4)}%`}
        />
      </Stack>
      <Divider />
      {ingress.queues.map((queue) => {
        const side = queue.side === 'clients' ? 'Frontside · clients' : 'Backside · backends';
        const queued = `${formatNumber(queue.stats.queuedPackets)} / ${formatNumber(queue.capacity)} packets`;
        const queuePercent = queue.capacity > 0
          ? Math.min(100, queue.stats.queuedPackets / queue.capacity * 100)
          : 0;
        const drops = [
          `${formatNumber(queue.stats.queueDroppedPackets)} packets`,
          formatBytes(queue.stats.queueDroppedBytes),
        ].join(' · ');

        return (
          <Box key={queue.id}>
            <Typography variant="subtitle2" sx={{ mb: 0.5 }}>{side}</Typography>
            <Stack component="dl" spacing={0.5} sx={{ m: 0 }}>
              <Fact label="Waiting / queue capacity" value={queued} />
              <Fact label="Tail drops total" value={drops} />
            </Stack>
            {queue.capacity > 0 && (
              <LinearProgress
                variant="determinate"
                value={queuePercent}
                aria-label={`${side} input queue occupancy`}
                sx={{ mt: 0.75 }}
              />
            )}
          </Box>
        );
      })}
      <Typography variant="caption" color="text.secondary">
        Waiting slots belong to each FIFO separately; active and travelling packets
        use no waiting slot. Zero slots require an immediate RX start.
        Both FIFOs compete for the same receiver.
      </Typography>
      <Divider />
      <Stack component="dl" spacing={1} sx={{ m: 0 }}>
        <Fact label="Waiting across both queues" value={waiting} />
        <Fact label="Shared RX stage" value={stats.transmitting ? 'Occupied' : 'Idle'} />
        <Fact label="Travelling after RX" value={`${formatNumber(stats.travellingPackets)} packets`} />
        <Fact label="Delivered at LB" value={delivered} />
        <Fact label="Random RX losses total" value={losses} />
      </Stack>
      <Typography variant="caption" color="text.secondary">
        Access arrows finish at these input queues. LB packet handling follows
        shared RX transmission and travel. RX travel and loss apply once.
      </Typography>
    </Stack>
  );
}

export default TopologyIngressDetails;
