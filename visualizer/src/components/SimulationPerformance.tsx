import { Box, Card, CardContent, CardHeader, Typography } from '@mui/material';
import { formatNumber } from '../topology/format';
import type { SimulationPerformanceMetrics } from '../types';
import HelpTooltip from './HelpTooltip';

interface SimulationPerformanceProps {
  performance?: SimulationPerformanceMetrics;
}

// Display session averages supplied by the backend without deriving rates from UI refreshes.
function SimulationPerformance({ performance }: SimulationPerformanceProps) {
  if (!performance) return null;

  const metrics = [
    { label: 'LB packets / real s', value: formatNumber(performance.forwardedPacketsPerWallSecond) },
    { label: 'Events / real s', value: formatNumber(performance.eventsPerWallSecond) },
    { label: 'Elapsed real time', value: `${formatNumber(performance.wallTimeSeconds, 3)} s` },
    { label: 'Events executed', value: formatNumber(performance.eventsExecuted, 0) },
    { label: 'LB packets forwarded', value: formatNumber(performance.forwardedPackets, 0) },
  ];

  return (
    <Card variant="outlined" sx={{ mt: 2 }}>
      <CardHeader
        title="Simulation performance"
        action={(
          <HelpTooltip label="Simulation performance">
            Average simulator work per real second since execution started, including pacing
            and snapshot work. LB packets are counted once when their outgoing transmission
            starts, including ACKs, retries and control packets. This measures simulator
            throughput, not Linux kernel load balancer capacity.
          </HelpTooltip>
        )}
      />
      <CardContent sx={{ pt: 0 }}>
        <Box
          component="dl"
          sx={{
            display: 'grid',
            gridTemplateColumns: {
              xs: 'repeat(2, minmax(0, 1fr))',
              lg: 'repeat(5, minmax(0, 1fr))',
            },
            gap: 2,
            m: 0,
          }}
        >
          {metrics.map(({ label, value }) => (
            <Box key={label} sx={{ minWidth: 0 }}>
              <Typography component="dt" variant="body2" color="text.secondary">
                {label}
              </Typography>
              <Typography
                component="dd"
                variant="h6"
                sx={{
                  m: 0,
                  fontSize: { xs: '1.1rem', sm: '1.25rem' },
                  fontVariantNumeric: 'tabular-nums',
                  overflowWrap: 'anywhere',
                }}
              >
                {value}
              </Typography>
            </Box>
          ))}
        </Box>
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1.5 }}>
          Rates are averages over real execution time. Use playback speed 0 for an unpaced throughput measurement.
        </Typography>
      </CardContent>
    </Card>
  );
}

export default SimulationPerformance;
