import { Stack, Tooltip, Typography } from '@mui/material';
import { formatBytesPerSecond, formatNumber } from './format';
import type { TopologyTraffic } from './types';

interface TopologyTrafficRatesProps {
  traffic?: TopologyTraffic;
  grouped: boolean;
}

// Show backend measurements directly, including an explicit gap before the first sample.
function TopologyTrafficRates({ traffic, grouped }: TopologyTrafficRatesProps) {
  const sampleWindow = traffic ? formatNumber(traffic.sampleWindowSeconds, 9) : null;
  const measurement = traffic
    ? `Average over the last measured window (${sampleWindow} virtual s), not a configured link limit.`
    : 'No measured virtual-time window is available.';
  const counts = 'TX counts completed wire-byte transmissions; RX counts delivered bytes. '
    + 'Headers, ACKs and retries are included.';
  const groupScope = grouped
    ? ' The rates sum only clients represented by this group rectangle.'
    : '';
  const description = `${measurement} ${counts}${groupScope}`;
  const rates = [
    { label: 'TX', value: traffic?.transmitBytesPerSecond },
    { label: 'RX', value: traffic?.receiveBytesPerSecond },
  ];

  return (
    <Tooltip title={description} describeChild arrow>
      <Stack
        role="group"
        aria-label="Measured wire traffic"
        tabIndex={0}
        spacing={0.25}
        sx={{ mt: 1, pt: 0.75, borderTop: '1px solid', borderColor: 'divider' }}
      >
        <Typography variant="caption" color="text.secondary" sx={{ fontSize: 10 }}>
          Measured traffic
        </Typography>
        {rates.map(({ label, value }) => (
          <Stack key={label} direction="row" justifyContent="space-between" spacing={1}>
            <Typography variant="caption" color="text.secondary">{label}</Typography>
            <Typography variant="caption" fontWeight={600}>
              {formatBytesPerSecond(value)}
            </Typography>
          </Stack>
        ))}
      </Stack>
    </Tooltip>
  );
}

export default TopologyTrafficRates;
