import { Box, Card, CardContent, CardHeader, Chip, Divider, Grid, Paper, Stack, Typography } from '@mui/material';
import HelpTooltip from '../components/HelpTooltip';
import type { LoadBalancerState } from '../types';

interface SimulationPageProps {
  data: LoadBalancerState | null;
}

function SimulationPage({ data }: SimulationPageProps) {
  return (
    <>
      {data ? (
        <Stack spacing={3} sx={{ mt: 3 }}>
          <Card variant="outlined">
            <CardHeader
              title="Load balancer"
              action={(
                <HelpTooltip label="Network topology">
                  All clients share one link to the balancer and another for replies. Each backend has its own link in each direction. Every link has its own bandwidth limit and packet queue, shared by the TCP connections using it.
                </HelpTooltip>
              )}
            />
            <CardContent>
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}>
                <Chip label={`Algorithm: ${data.algorithm}`} color="primary" variant="outlined" />
                <HelpTooltip label="Current algorithm">
                  The policy currently used to choose a backend for each new TCP connection. Established connections keep their assigned backend.
                </HelpTooltip>
              </Stack>
            </CardContent>
          </Card>

          <Card variant="outlined">
            <CardHeader title={`Backend servers (${data.servers.length})`} />
            <CardContent>
              <Grid container spacing={2}>
                {data.servers.map((serverData) => {
                  const { server, clients } = serverData;
                  const active = server.Active;
                  return (
                    <Grid size={{ xs: 12, sm: 6, md: 4 }} key={server.ID} sx={{ display: 'flex' }}>
                      <Card variant="outlined" sx={{ width: '100%', display: 'flex', flexDirection: 'column' }}>
                        <CardHeader
                          title={server.Name}
                          subheader={`${server.Address}:${server.Port}`}
                          action={(
                            <Stack direction="row" spacing={0.5} alignItems="center">
                              <Chip label={active ? 'Active' : 'Inactive'} color={active ? 'success' : 'default'} size="small" />
                              <HelpTooltip label="Backend activity">
                                Whether this simulated server is enabled to handle packets. The balancer also checks health and connection capacity when choosing a backend.
                              </HelpTooltip>
                            </Stack>
                          )}
                        />
                        <CardContent sx={{ flexGrow: 1 }}>
                          <Stack spacing={1}>
                            <Stack direction="row" spacing={0.5} alignItems="center">
                              <Typography variant="body2">Established connections: {server.ActiveConnections}</Typography>
                              <HelpTooltip label="Established connections">
                                TCP connections whose handshake has completed at this server and that have not yet closed. Many connections can be open at the same virtual time.
                              </HelpTooltip>
                            </Stack>
                            <Stack direction="row" spacing={0.5} alignItems="center">
                              <Typography variant="body2">Half-open connections: {server.HalfOpenConnections}</Typography>
                              <HelpTooltip label="Half-open connections">
                                Connections for which the server received a SYN and is still waiting for the final handshake acknowledgement.
                              </HelpTooltip>
                            </Stack>
                            <Stack direction="row" spacing={0.5} alignItems="center">
                              <Typography variant="body2">P95 connection lifetime: {server.LatencyP95Milli} ms</Typography>
                              <HelpTooltip label="P95 connection lifetime">
                                95% of the last up to 100 connections completed with FIN had lifetimes at or below this value. Measured in virtual milliseconds from SYN arrival to FIN arrival at the server. Zero can mean there are no samples yet.
                              </HelpTooltip>
                            </Stack>
                            <Stack direction="row" spacing={0.5} alignItems="center">
                              <Typography variant="body2">Packets received: {server.NumberOfHandledPackets}</Typography>
                              <HelpTooltip label="Packets received">
                                All incoming TCP packets reaching this active server during the run, including handshake, data, closing packets and retransmissions. Includes packets the server later rejects.
                              </HelpTooltip>
                            </Stack>
                            <Divider />
                            <Stack direction="row" spacing={0.5} alignItems="center">
                              <Typography variant="subtitle2">Routed clients ({clients.length})</Typography>
                              <HelpTooltip label="Routed clients">
                                Clients currently assigned to this backend, including connections still completing their handshake. Entries disappear when the balancer removes the connection. Addresses and ports identify simulated endpoints.
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
                    </Grid>
                  );
                })}
              </Grid>
            </CardContent>
          </Card>
        </Stack>
      ) : (
        <Paper variant="outlined" sx={{ mt: 3, p: 3, textAlign: 'center' }}>
          <Typography variant="body1">Waiting for simulation state…</Typography>
        </Paper>
      )}
    </>
  );
}

export default SimulationPage;
