import {
  Box,
  Card,
  CardContent,
  CardHeader,
  Divider,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { formatNumber } from '../topology/format';
import type { DurationSummary, SimulationTransferResults, TransferFailureReason } from '../types';
import HelpTooltip from './HelpTooltip';

interface TransferResultsProps {
  transfers?: SimulationTransferResults;
}

const failureLabels: Record<TransferFailureReason, string> = {
  synRetryLimit: 'SYN retry limit reached',
  dataRetryLimit: 'Data retry limit reached',
  readTimeout: 'Read timeout',
  remoteClosed: 'Remote closed before completion',
  closedEarly: 'Endpoint closed before completion',
  interrupted: 'Transfer interrupted',
  balancerTimeout: 'Balancer connection timeout',
  backendRemoved: 'Backend removed',
};

function durationValue(summary: DurationSummary | undefined, value: number | undefined): string {
  if (!summary || value === undefined) return 'Unknown';
  if (summary.samples === 0) return 'Unavailable';
  // Significant digits keep short, nonzero durations visible.
  return `${value.toLocaleString('en-US', { maximumSignificantDigits: 4 })} s`;
}

function TransferResults({ transfers }: TransferResultsProps) {
  const counts = [
    { label: 'Started', value: transfers?.started },
    { label: 'Succeeded', value: transfers?.succeeded },
    { label: 'Failed', value: transfers?.failed },
    { label: 'In progress', value: transfers?.inProgress },
    { label: 'Not started', value: transfers?.notStarted },
    { label: 'SYN retransmissions', value: transfers?.synRetransmissions },
    { label: 'Data retransmissions', value: transfers?.dataRetransmissions },
  ];
  const durations = [
    {
      label: 'Handshake',
      summary: transfers?.handshake,
      help: 'From the connection request (the same virtual timestamp as the initial SYN) '
        + 'to the first accepted SYN-ACK. Includes SYN retries. Samples include transfers '
        + 'that completed the handshake, even if they later fail.',
    },
    {
      label: 'Payload',
      summary: transfers?.payload,
      help: 'Successful transfers only: from the first original data send to the final payload ACK. '
        + 'Excludes the initial sender pause; includes later pauses and retries. '
        + 'A successful zero-payload transfer has duration 0.',
    },
    {
      label: 'Completion',
      summary: transfers?.completion,
      help: 'Successful transfers only: from the connection attempt start to the final payload ACK, '
        + 'including the handshake and initial sender pause.',
    },
  ];
  const failures = Object.entries(transfers?.failures ?? {}).filter(([, count]) => count > 0);

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardHeader
        title="Transfer results"
        action={(
          <HelpTooltip label="Transfer results">
            Each started client is one transfer. Success means all payload is ACK-confirmed,
            independently of later FIN or connection closure. Failure means an existing endpoint
            aborted before completion. Reaching the virtual horizon leaves active transfers in
            progress. Not started counts scheduled arrivals not yet created.
            Retransmissions are cumulative for this run, until reset.
          </HelpTooltip>
        )}
      />
      <CardContent sx={{ pt: 0 }}>
        <Box
          component="dl"
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: 'repeat(2, minmax(0, 1fr))', sm: 'repeat(5, minmax(0, 1fr))' },
            gap: 2,
            m: 0,
          }}
        >
          {counts.map(({ label, value }) => (
            <Box key={label} sx={{ minWidth: 0 }}>
              <Typography component="dt" variant="body2" color="text.secondary">
                {label}
              </Typography>
              <Typography
                component="dd"
                variant="h6"
                sx={{ m: 0, fontVariantNumeric: 'tabular-nums', overflowWrap: 'anywhere' }}
              >
                {value === undefined ? 'Unknown' : formatNumber(value, 0)}
              </Typography>
            </Box>
          ))}
        </Box>

        <Divider sx={{ my: 2 }} />
        <Typography variant="subtitle2">Virtual durations</Typography>
        <Typography variant="caption" color="text.secondary">
          Payload and completion include successes only. Handshake includes every completed handshake.
        </Typography>
        <TableContainer>
          <Table size="small" aria-label="Transfer virtual durations">
            <TableHead>
              <TableRow>
                <TableCell>Phase</TableCell>
                <TableCell align="right">Samples</TableCell>
                <TableCell align="right">Mean</TableCell>
                <TableCell align="right">Min</TableCell>
                <TableCell align="right">Max</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {durations.map(({ label, summary, help }) => (
                <TableRow key={label} sx={{ '& > *': { fontVariantNumeric: 'tabular-nums' } }}>
                  <TableCell component="th" scope="row" sx={{ pl: 0 }}>
                    <Stack direction="row" spacing={0.5} alignItems="center">
                      <span>{label}</span>
                      <HelpTooltip label={`${label} duration`}>{help}</HelpTooltip>
                    </Stack>
                  </TableCell>
                  <TableCell align="right">
                    {summary ? formatNumber(summary.samples, 0) : 'Unknown'}
                  </TableCell>
                  <TableCell align="right">{durationValue(summary, summary?.meanSeconds)}</TableCell>
                  <TableCell align="right">{durationValue(summary, summary?.minSeconds)}</TableCell>
                  <TableCell align="right">{durationValue(summary, summary?.maxSeconds)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>

        <Typography variant="subtitle2" sx={{ mt: 2 }}>Failure reasons</Typography>
        {transfers === undefined ? (
          <Typography variant="body2" color="text.secondary">Unknown</Typography>
        ) : failures.length === 0 ? (
          <Typography variant="body2" color="text.secondary">None</Typography>
        ) : (
          <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
            {failures.map(([reason, count]) => (
              <Typography component="li" variant="body2" key={reason}>
                {failureLabels[reason as TransferFailureReason] ?? 'Unspecified failure'}: {formatNumber(count, 0)}
              </Typography>
            ))}
          </Box>
        )}
      </CardContent>
    </Card>
  );
}

export default TransferResults;
