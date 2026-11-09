import {
  Alert, Box, Button, Chip, Grid, MenuItem, Paper, Stack, TextField, Typography,
} from '@mui/material';
import { useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { Link as RouterLink } from 'react-router';
import {
  formatVirtualSeconds, parseVirtualSeconds, projectPlannedTopology,
} from '../scenarios/plannedTopology';
import type { Scenario } from '../scenarios/types';
import PlannedResourceDetails from '../topology/PlannedResourceDetails';

interface PlannedTopologyPageProps {
  scenario: Scenario | null;
  error: string | null;
}

const resourceButtonStyle = {
  justifyContent: 'flex-start',
  textAlign: 'left',
  textTransform: 'none',
  overflowWrap: 'anywhere',
} as const;

// This view consumes declared settings only. It has no connection to runtime counters.
function PlannedTopologyPage({ scenario, error }: PlannedTopologyPageProps) {
  return (
    <Stack spacing={2.5} sx={{ py: 3 }}>
      <Box>
        <Typography variant="h5" component="h1" fontWeight={700}>
          Planned topology
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          Inspect the valid applied scenario before Start. Import a file, apply a preset or edit
          the form on the Simulation tab. This preview does not run the engine.
        </Typography>
      </Box>
      <Alert severity="info">
        Declared configuration only: generator budgets are not active connections. Clients already
        created can survive a later budget decrease or group removal. Traffic, drops and observed
        backend health are available in Live topology after a run starts.
      </Alert>
      {error && (
        <Alert severity="warning" sx={{ overflowWrap: 'anywhere' }}>
          {scenario ? 'Showing the last valid applied scenario. ' : 'No valid scenario to preview. '}
          {error}
          {' '}
          <Button component={RouterLink} to="/" color="inherit" size="small">
            Open form
          </Button>
        </Alert>
      )}
      {scenario ? <PlannedTimeline scenario={scenario} /> : (
        <Alert severity="info">Waiting for valid scenario defaults or an imported scenario.</Alert>
      )}
    </Stack>
  );
}

function PlannedTimeline({ scenario }: { scenario: Scenario }) {
  const [timeText, setTimeText] = useState('0');
  const [selectedTime, setSelectedTime] = useState(0n);
  const [selection, setSelection] = useState('loadBalancer');
  const duration = BigInt(scenario.fixed.execution.durationNanoseconds);
  const at = selectedTime > duration ? duration : selectedTime;
  const plan = useMemo(() => projectPlannedTopology(scenario, at), [scenario, at]);
  let timeError: string | null = null;
  try {
    if (parseVirtualSeconds(timeText) > duration) {
      timeError = `Enter a time from 0 to ${formatVirtualSeconds(duration)} seconds.`;
    }
  } catch (failure) {
    timeError = failure instanceof Error ? failure.message : 'Enter a valid virtual time.';
  }

  function changeTime(text: string) {
    setTimeText(text);
    try {
      const requested = parseVirtualSeconds(text);
      if (requested <= duration) setSelectedTime(requested);
    } catch {
      // Keep the previous valid instant while unfinished text is visible.
    }
  }

  function jump(atNanoseconds: string) {
    const nextTime = BigInt(atNanoseconds);
    setSelectedTime(nextTime);
    setTimeText(formatVirtualSeconds(nextTime));
  }

  const resources = [
    { id: 'loadBalancer', label: `LB: ${plan.loadBalancer.id}` },
    ...plan.clientGroups.map(group => ({
      id: `group:${group.id}`,
      label: `Generator: ${group.id}`,
    })),
    ...plan.backends.map(backend => ({
      id: `backend:${backend.id}`,
      label: `Backend: ${backend.name || backend.id}`,
    })),
  ];
  const selectedResource = resources.some(resource => resource.id === selection) ? selection : 'loadBalancer';

  return (
    <>
      <Paper variant="outlined" sx={{ p: 2 }}>
        <Stack spacing={2}>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Chip label="Planned configuration" color="info" size="small" />
            <Chip label={`${Object.keys(scenario.states).length} declared states`} size="small" />
          </Stack>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} alignItems="flex-start">
            <TextField
              label="Virtual time (seconds)"
              value={timeText}
              onChange={event => changeTime(event.target.value)}
              error={timeError !== null}
              helperText={timeError ?? `0–${formatVirtualSeconds(duration)} s; up to 9 decimal places`}
              slotProps={{ htmlInput: { inputMode: 'decimal' } }}
              sx={{ minWidth: 260 }}
            />
            <Button
              variant="outlined"
              disabled={plan.previousAtNanoseconds === null}
              onClick={() => plan.previousAtNanoseconds !== null && jump(plan.previousAtNanoseconds)}
            >
              Previous state
            </Button>
            <Button
              variant="outlined"
              disabled={plan.nextAtNanoseconds === null}
              onClick={() => plan.nextAtNanoseconds !== null && jump(plan.nextAtNanoseconds)}
            >
              Next state
            </Button>
          </Stack>
          <Typography variant="body2" role="status">
            Preview at {formatVirtualSeconds(plan.atNanoseconds)} s ({plan.atNanoseconds} ns).
            {' '}Using declared state at {formatVirtualSeconds(plan.stateAtNanoseconds)} s
            {' '}({plan.stateAtNanoseconds} ns).
          </Typography>
          <Typography variant="caption" color="text.secondary">
            The latest state at or before the selected instant supplies the complete configuration.
            Previous and Next jump to declared states; selecting a time never advances a simulation.
          </Typography>
        </Stack>
      </Paper>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 4 }}>
          <ResourceColumn
            title="Client generators →"
            count={`${plan.clientGroups.length} declared groups`}
          >
            {plan.clientGroups.length === 0 && (
              <Typography variant="body2">No generators declared in this state.</Typography>
            )}
            {plan.clientGroups.slice(0, 8).map(group => (
              <Button
                key={group.id}
                variant={selectedResource === `group:${group.id}` ? 'contained' : 'outlined'}
                onClick={() => setSelection(`group:${group.id}`)}
                sx={resourceButtonStyle}
              >
                {group.id}: {BigInt(group.count).toLocaleString('en-US')} lifetime birth budget
              </Button>
            ))}
            {plan.clientGroups.length > 8 && (
              <Typography variant="caption">Choose any group in the resource selector below.</Typography>
            )}
          </ResourceColumn>
        </Grid>
        <Grid size={{ xs: 12, md: 4 }}>
          <ResourceColumn title="Load balancer →" count="Shared RX, CPU and forwarding resources">
            <Button
              variant={selectedResource === 'loadBalancer' ? 'contained' : 'outlined'}
              onClick={() => setSelection('loadBalancer')}
              sx={resourceButtonStyle}
            >
              {plan.loadBalancer.id} · {plan.loadBalancer.algorithm}
            </Button>
            <Typography variant="body2">
              Both input sides share one ingress limit. All output directions share one forwarding budget.
            </Typography>
          </ResourceColumn>
        </Grid>
        <Grid size={{ xs: 12, md: 4 }}>
          <ResourceColumn title="Backends" count={`${plan.backends.length} declared resources`}>
            {plan.backends.length === 0 && (
              <Typography variant="body2">No backends declared in this state.</Typography>
            )}
            {plan.backends.slice(0, 8).map(backend => (
              <Button
                key={backend.id}
                variant={selectedResource === `backend:${backend.id}` ? 'contained' : 'outlined'}
                onClick={() => setSelection(`backend:${backend.id}`)}
                sx={resourceButtonStyle}
              >
                {backend.name || backend.id}
                {' · '}{backend.physicalAvailable ? 'available' : 'unavailable'}
                {' · '}{backend.administrativeState}
              </Button>
            ))}
            {plan.backends.length > 8 && (
              <Typography variant="caption">Choose any backend in the resource selector below.</Typography>
            )}
          </ResourceColumn>
        </Grid>
      </Grid>

      <Paper variant="outlined" sx={{ p: 2 }}>
        <Stack spacing={2}>
          <TextField
            select
            label="Inspect configured resource"
            value={selectedResource}
            onChange={event => setSelection(event.target.value)}
            fullWidth
          >
            {resources.map(resource => (
              <MenuItem key={resource.id} value={resource.id}>{resource.label}</MenuItem>
            ))}
          </TextField>
          <PlannedResourceDetails plan={plan} selection={selectedResource} />
        </Stack>
      </Paper>
    </>
  );
}

function ResourceColumn({ title, count, children }: {
  title: string;
  count: string;
  children: ReactNode;
}) {
  return (
    <Paper variant="outlined" sx={{ p: 2, height: '100%' }}>
      <Stack spacing={1.5}>
        <Typography variant="h6" component="h2">{title}</Typography>
        <Typography variant="caption" color="text.secondary">{count}</Typography>
        {children}
      </Stack>
    </Paper>
  );
}

export default PlannedTopologyPage;
