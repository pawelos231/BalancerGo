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
import type { Payload } from '../App';
import { CommandType } from '../commands';
import type { AlgorithmName, SimulationMetadata } from '../types';
import HelpTooltip from './HelpTooltip';
import SimulationForm from './SimulationForm';

interface ControlPanelProps {
  connected: boolean;
  simulation: SimulationMetadata | null;
  startPending: boolean;
  stopPending: boolean;
  commandError: string | null;
  sendMessage: (payload: Payload) => boolean;
}

const statusLabels: Record<SimulationMetadata['status'], string> = {
  idle: 'Ready to start',
  running: 'Simulation running',
  finished: 'Simulation finished',
  failed: 'Simulation failed',
};

function ControlPanel({ connected, simulation, startPending, stopPending, commandError, sendMessage }: ControlPanelProps) {
  const [algorithm, setAlgorithm] = useState<AlgorithmName | null>(null);
  const [clientCount, setClientCount] = useState('1');
  const lifecyclePending = startPending || stopPending;
  const actionsEnabled = connected && simulation?.status === 'running' && !lifecyclePending;
  const selectedAlgorithm = algorithm ?? simulation?.config.algorithm ?? '';

  const count = Number(clientCount);
  const validCount = clientCount.trim() !== '' && Number.isSafeInteger(count) && count >= 1 && count <= 10000;

  return (
    <Box sx={{ width: '100%', pt: 2 }}>
      <Stack spacing={2}>
        {simulation && (
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}>
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Chip
                label={statusLabels[simulation.status]}
                color={simulation.status === 'running' ? 'primary' : simulation.status === 'failed' ? 'error' : 'default'}
              />
              <HelpTooltip label="About the shared experiment">
                All connected browsers view and control the same experiment. Starting, changing or stopping it affects every connected browser.
              </HelpTooltip>
            </Stack>
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Typography variant="body2">
                Virtual time: {simulation.virtualTimeSeconds.toLocaleString('en-US', { maximumFractionDigits: 3 })} s
              </Typography>
              <HelpTooltip label="About virtual time">
                Time inside the simulation, separate from elapsed real time. At speed 0, it advances without real-time waits.
              </HelpTooltip>
            </Stack>
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Button
                variant="outlined"
                color="warning"
                disabled={!connected || simulation.status === 'idle' || lifecyclePending}
                onClick={() => sendMessage({ Type: CommandType.StopSimulation, Payload: {} })}
              >
                {stopPending ? 'Stopping…' : 'Stop and clear'}
              </Button>
              <HelpTooltip label="About stopping and clearing">
                Clear queued events and packets, link and TCP connection state, and counters. Reset virtual time to 0; keep your configuration draft.
              </HelpTooltip>
            </Stack>
          </Stack>
        )}
        {(commandError || simulation?.error) && <Alert severity="error">{commandError || simulation?.error}</Alert>}
        {simulation ? (
          <SimulationForm
            initialConfig={simulation.config}
            connected={connected}
            running={simulation.status === 'running'}
            pending={lifecyclePending}
            onStart={(config) => sendMessage({ Type: CommandType.StartSimulation, Payload: config })}
          />
        ) : (
          <Alert severity="info">Waiting for simulation settings from the server…</Alert>
        )}
      </Stack>
      <Grid container spacing={2} sx={{ width: '100%', mt: 2 }}>
        <Grid size={{ xs: 12, sm: 6 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardHeader
              title="Algorithm during the run"
              action={(
                <HelpTooltip label="About choosing a backend">
                  Applies to new TCP connections. Existing connections keep their backend. Round Robin cycles through available backends; Random uses the experiment's seed.
                </HelpTooltip>
              )}
            />
            <CardContent>
              <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} alignItems="center">
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
                  onClick={() => sendMessage({ Type: CommandType.SetAlgorithm, Payload: { Name: selectedAlgorithm } })}
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
                  Create the entered number of new simulated TCP clients. They use the current run's settings and each starts its own connection.
                </HelpTooltip>
              )}
            />
            <CardContent>
              <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} alignItems="flex-start">
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
                  helperText={!validCount ? 'Enter an integer from 1 to 10000.' : undefined}
                />
                <Button
                  variant="contained"
                  disabled={!actionsEnabled || !validCount}
                  onClick={() => sendMessage({ Type: CommandType.SpawnClient, Payload: { Count: count } })}
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
