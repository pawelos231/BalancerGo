import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import { useState } from 'react';
import { CommandType } from '../commands';
import type {
  AlgorithmName,
  SimulationCommand,
  SimulationConfig,
  SimulationMetadata,
} from '../types';
import HelpTooltip from './HelpTooltip';
import SimulationForm from './SimulationForm';

interface ControlPanelProps {
  connected: boolean;
  simulation: SimulationMetadata | null;
  startPending: boolean;
  stopPending: boolean;
  commandError: string | null;
  sendMessage: (command: SimulationCommand) => boolean;
}

const statusLabels: Record<SimulationMetadata['status'], string> = {
  idle: 'Ready to start',
  running: 'Simulation running',
  finished: 'Simulation finished',
  failed: 'Simulation failed',
};

function statusColor(status: SimulationMetadata['status']) {
  if (status === 'running') {
    return 'primary';
  }

  if (status === 'failed') {
    return 'error';
  }

  return 'default';
}

function ControlPanel({
  connected,
  simulation,
  startPending,
  stopPending,
  commandError,
  sendMessage,
}: ControlPanelProps) {
  const [algorithm, setAlgorithm] = useState<AlgorithmName | null>(null);
  const [clientCount, setClientCount] = useState('1');

  const running = simulation?.status === 'running';
  const lifecyclePending = startPending || stopPending;
  const actionsEnabled = connected && running && !lifecyclePending;
  const canStop = connected && simulation !== null
    && simulation.status !== 'idle' && !lifecyclePending;

  const selectedAlgorithm = algorithm ?? simulation?.config.algorithm ?? '';
  const virtualTime = simulation?.virtualTimeSeconds.toLocaleString('en-US', {
    maximumFractionDigits: 3,
  });
  const visibleError = commandError || simulation?.error;
  const stopLabel = stopPending ? 'Stopping…' : 'Stop and clear';

  const count = Number(clientCount);
  const validCount = clientCount.trim() !== ''
    && Number.isSafeInteger(count)
    && count >= 1
    && count <= 10000;
  const countError = validCount ? undefined : 'Enter an integer from 1 to 10000.';

  function startSimulation(config: SimulationConfig) {
    return sendMessage({
      Type: CommandType.StartSimulation,
      Payload: config,
    });
  }

  function stopSimulation() {
    sendMessage({
      Type: CommandType.StopSimulation,
      Payload: {},
    });
  }

  function applyAlgorithm() {
    sendMessage({
      Type: CommandType.SetAlgorithm,
      Payload: { Name: selectedAlgorithm },
    });
  }

  function addClients() {
    sendMessage({
      Type: CommandType.SpawnClient,
      Payload: { Count: count },
    });
  }

  return (
    <Box sx={{ width: '100%', pt: 2 }}>
      <Stack spacing={2}>
        {simulation && (
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={1}
            alignItems={{ sm: 'center' }}
          >
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Chip
                label={statusLabels[simulation.status]}
                color={statusColor(simulation.status)}
              />

              <HelpTooltip label="About the shared experiment">
                All connected browsers view and control the same experiment.
                Starting, changing or stopping it affects every connected browser.
              </HelpTooltip>
            </Stack>

            <Stack direction="row" spacing={0.5} alignItems="center">
              <Typography variant="body2">
                Virtual time: {virtualTime} s
              </Typography>

              <HelpTooltip label="About virtual time">
                Time inside the simulation, separate from elapsed real time.
                At speed 0, it advances without real-time waits.
              </HelpTooltip>
            </Stack>

            <Stack direction="row" spacing={0.5} alignItems="center">
              <Button
                variant="outlined"
                color="warning"
                disabled={!canStop}
                onClick={stopSimulation}
              >
                {stopLabel}
              </Button>

              <HelpTooltip label="About stopping and clearing">
                Clear queued events and packets, link and TCP connection state,
                and counters. Reset virtual time to 0; keep your configuration draft.
              </HelpTooltip>
            </Stack>
          </Stack>
        )}

        {visibleError && (
          <Alert severity="error">
            {visibleError}
          </Alert>
        )}

        {simulation ? (
          <SimulationForm
            initialConfig={simulation.config}
            connected={connected}
            running={running}
            pending={lifecyclePending}
            onStart={startSimulation}
          />
        ) : (
          <Alert severity="info">
            Waiting for simulation settings from the server…
          </Alert>
        )}
      </Stack>

      <Grid container spacing={2} sx={{ width: '100%', mt: 2 }}>
        <Grid size={{ xs: 12, sm: 6 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardHeader
              title="Algorithm during the run"
              action={(
                <HelpTooltip label="About choosing a backend">
                  Applies to new TCP connections. Existing connections keep their backend.
                  Round Robin cycles through available backends; Random uses the experiment's seed.
                </HelpTooltip>
              )}
            />

            <CardContent>
              <Stack
                direction={{ xs: 'column', md: 'row' }}
                spacing={1}
                alignItems="center"
              >
                <FormControl fullWidth size="small" disabled={!actionsEnabled}>
                  <InputLabel id="alg-label">Algorithm</InputLabel>

                  <Select
                    labelId="alg-label"
                    label="Algorithm"
                    value={selectedAlgorithm}
                    onChange={(event) => setAlgorithm(event.target.value as AlgorithmName)}
                  >
                    <MenuItem value="roundRobin">Round Robin</MenuItem>
                    <MenuItem value="random">Random</MenuItem>
                  </Select>
                </FormControl>

                <Button
                  variant="contained"
                  disabled={!actionsEnabled}
                  onClick={applyAlgorithm}
                >
                  Apply
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, sm: 6 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardHeader
              title="Add TCP clients during the run"
              action={(
                <HelpTooltip label="About adding TCP clients">
                  Create the entered number of new simulated TCP clients.
                  They use the current run's settings and each starts its own connection.
                </HelpTooltip>
              )}
            />

            <CardContent>
              <Stack
                direction={{ xs: 'column', md: 'row' }}
                spacing={1}
                alignItems="flex-start"
              >
                <TextField
                  fullWidth
                  size="small"
                  type="number"
                  label="Additional clients"
                  value={clientCount}
                  disabled={!actionsEnabled}
                  onChange={(event) => setClientCount(event.target.value)}
                  inputProps={{ min: 1, max: 10000, step: 1 }}
                  error={!validCount}
                  helperText={countError}
                />

                <Button
                  variant="contained"
                  disabled={!actionsEnabled || !validCount}
                  onClick={addClients}
                >
                  Add clients
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

export default ControlPanel;
