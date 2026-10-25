import {
  Box,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Divider,
  Stack,
  Typography,
} from '@mui/material';
import type { ServerStateWithClients } from '../types';
import { formatBytes, formatNumber } from '../topology/format';
import HelpTooltip from './HelpTooltip';

interface BackendMetricProps {
  label: string;
  value: number | undefined;
  unit?: string;
  children: string;
}

function BackendMetric({ label, value, unit, children }: BackendMetricProps) {
  let displayValue = 'Unknown';
  if (value !== undefined) {
    displayValue = unit === 'bytes' ? formatBytes(value) : `${value}${unit ? ` ${unit}` : ''}`;
  }
  const exactBytes = unit === 'bytes' && value !== undefined
    ? `${formatNumber(value, 0)} bytes`
    : undefined;

  return (
    <Stack direction="row" spacing={0.5} alignItems="center">
      <Typography variant="body2" title={exactBytes}>
        {label}: {displayValue}
      </Typography>

      <HelpTooltip label={label}>
        {children}
      </HelpTooltip>
    </Stack>
  );
}

function BackendCard({ server, clients }: ServerStateWithClients) {
  const active = server.Active;

  return (
    <Card
      variant="outlined"
      sx={{ width: '100%', display: 'flex', flexDirection: 'column' }}
    >
      <CardHeader
        title={server.Name}
        subheader={`${server.Address}:${server.Port}`}
        action={(
          <Stack direction="row" spacing={0.5} alignItems="center">
            <Chip
              label={active ? 'Active' : 'Inactive'}
              color={active ? 'success' : 'default'}
              size="small"
            />

            <HelpTooltip label="Backend activity">
              Whether this simulated server is enabled to handle packets.
              The balancer also checks health and connection capacity when choosing a backend.
            </HelpTooltip>
          </Stack>
        )}
      />

      <CardContent sx={{ flexGrow: 1 }}>
        <Stack spacing={1}>
          <BackendMetric label="Established connections" value={server.ActiveConnections}>
            TCP connections whose handshake has completed at this server and that have not yet closed.
            Many connections can be open at the same virtual time.
          </BackendMetric>

          <BackendMetric label="Half-open connections" value={server.HalfOpenConnections}>
            Connections for which the server received a SYN and is still waiting
            for the final handshake acknowledgement.
          </BackendMetric>

          <BackendMetric label="P95 connection lifetime" value={server.LatencyP95Milli} unit="ms">
            95% of the last up to 100 connections completed with FIN had lifetimes at or below this value.
            Measured in virtual milliseconds from SYN arrival to FIN arrival at the server.
            Zero can mean there are no samples yet.
          </BackendMetric>

          <BackendMetric label="Packets received" value={server.NumberOfHandledPackets}>
            All incoming TCP packets reaching this active server during the run,
            including handshake, data, closing packets and retransmissions.
            Includes packets the server later rejects.
          </BackendMetric>

          <BackendMetric label="Payload received" value={server.ReceivedPayloadBytes} unit="bytes">
            Cumulative unique payload bytes received during this run, including unfinished transfers.
            Excludes retransmissions, headers and control packets.
            Remains counted after connections close, until reset.
          </BackendMetric>

          <Divider />

          <Stack direction="row" spacing={0.5} alignItems="center">
            <Typography variant="subtitle2">
              Routed clients ({clients.length})
            </Typography>

            <HelpTooltip label="Routed clients">
              Clients currently assigned to this backend, including connections still completing their handshake.
              Entries disappear when the balancer removes the connection.
              Addresses and ports identify simulated endpoints.
            </HelpTooltip>
          </Stack>

          <Box
            sx={{
              maxHeight: 180,
              overflowY: 'auto',
              border: '1px solid',
              borderColor: 'divider',
              borderRadius: 1,
              p: 1,
              bgcolor: 'background.paper',
            }}
          >
            <Stack spacing={1}>
              {clients.map((client, index) => (
                <Box
                  key={`${server.ID}-${index}`}
                  component="pre"
                  sx={{
                    m: 0,
                    p: 1,
                    bgcolor: 'action.hover',
                    borderRadius: 1,
                    fontSize: '0.85rem',
                    whiteSpace: 'pre-wrap',
                  }}
                >
                  {JSON.stringify(client.Key, null, 2)}
                </Box>
              ))}
            </Stack>
          </Box>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default BackendCard;
