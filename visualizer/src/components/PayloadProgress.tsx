import { Box, Card, CardContent, CardHeader, Typography } from '@mui/material';
import { formatBytes, formatNumber } from '../topology/format';
import type { SimulationPayloadProgress } from '../types';
import HelpTooltip from './HelpTooltip';

interface PayloadProgressProps {
  payload?: SimulationPayloadProgress;
}

function PayloadProgress({ payload }: PayloadProgressProps) {
  const metrics = [
    { label: 'Received by backends', value: payload?.receivedBytes },
    { label: 'ACK-confirmed by clients', value: payload?.acknowledgedBytes },
  ];

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardHeader
        title="Payload progress"
        action={(
          <HelpTooltip label="Payload progress">
            Cumulative unique payload bytes for this run, including unfinished transfers.
            Excludes retransmissions, headers and control packets. The totals can differ
            while ACKs are in flight or lost. Bytes remain counted after connections close,
            until reset.
          </HelpTooltip>
        )}
      />
      <CardContent sx={{ pt: 0 }}>
        <Box
          component="dl"
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' },
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
                title={value === undefined ? undefined : `${formatNumber(value, 0)} bytes`}
                sx={{ m: 0, fontVariantNumeric: 'tabular-nums', overflowWrap: 'anywhere' }}
              >
                {value === undefined ? 'Unknown' : formatBytes(value)}
              </Typography>
            </Box>
          ))}
        </Box>
      </CardContent>
    </Card>
  );
}

export default PayloadProgress;
