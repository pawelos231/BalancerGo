import {
  Card,
  CardContent,
  CardHeader,
  Chip,
  Grid,
  Paper,
  Stack,
  Typography,
} from '@mui/material';
import BackendCard from '../components/BackendCard';
import HelpTooltip from '../components/HelpTooltip';
import type { LoadBalancerState } from '../types';

interface SimulationPageProps {
  data: LoadBalancerState | null;
}

function SimulationPage({ data }: SimulationPageProps) {
  if (!data) {
    return (
      <Paper variant="outlined" sx={{ mt: 3, p: 3, textAlign: 'center' }}>
        <Typography variant="body1">
          Waiting for simulation state…
        </Typography>
      </Paper>
    );
  }

  return (
    <Stack spacing={3} sx={{ mt: 3 }}>
      <Card variant="outlined">
        <CardHeader
          title="Load balancer"
          action={(
            <HelpTooltip label="Network topology">
              Clients and backends connect to opposite sides of the load balancer.
              Their separate input queues share one input bandwidth limit.
              Outgoing links keep their own queues and share one forwarding budget.
            </HelpTooltip>
          )}
        />

        <CardContent>
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={1}
            alignItems={{ sm: 'center' }}
          >
            <Chip
              label={`Algorithm: ${data.algorithm}`}
              color="primary"
              variant="outlined"
            />

            <HelpTooltip label="Current algorithm">
              The policy currently used to choose a backend for each new TCP connection.
              Established connections keep their assigned backend.
            </HelpTooltip>
          </Stack>
        </CardContent>
      </Card>

      <Card variant="outlined">
        <CardHeader title={`Backend servers (${data.servers.length})`} />

        <CardContent>
          <Grid container spacing={2}>
            {data.servers.map(({ server, clients }) => (
              <Grid
                size={{ xs: 12, sm: 6, md: 4 }}
                key={server.ID}
                sx={{ display: 'flex' }}
              >
                <BackendCard server={server} clients={clients} />
              </Grid>
            ))}
          </Grid>
        </CardContent>
      </Card>
    </Stack>
  );
}

export default SimulationPage;
