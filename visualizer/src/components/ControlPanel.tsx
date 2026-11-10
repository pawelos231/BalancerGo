import {
  Alert,
  Box,
  Button,
  Chip,
  Stack,
  Typography,
} from '@mui/material';
import type { ReactNode } from 'react';
import { CommandType } from '../commands';
import type {
  SimulationCommand,
  SimulationMetadata,
} from '../types';
import HelpTooltip from './HelpTooltip';

interface ControlPanelProps {
  connected: boolean;
  simulation: SimulationMetadata | null;
  startPending: boolean;
  stopPending: boolean;
  commandError: string | null;
  sendMessage: (command: SimulationCommand) => boolean;
  onStopAndSave: () => boolean;
  children: ReactNode;
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
  onStopAndSave,
  children,
}: ControlPanelProps) {
  const lifecyclePending = startPending || stopPending;
  const canStop = connected && simulation !== null
    && simulation.status !== 'idle' && !lifecyclePending;

  const virtualTime = simulation?.virtualTimeSeconds.toLocaleString('en-US', {
    maximumFractionDigits: 3,
  });
  const visibleError = commandError || simulation?.error;
  const stopLabel = stopPending ? 'Stopping…' : 'Stop and clear';

  function stopSimulation() {
    sendMessage({
      Type: CommandType.StopSimulation,
      Payload: {},
    });
  }

  return (
    <Box sx={{ width: '100%', pt: 2 }}>
      <Stack spacing={2}>
        {simulation && (
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={1}
            useFlexGap
            sx={{ flexWrap: 'wrap' }}
            alignItems={{ sm: 'center' }}
          >
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Chip
                label={statusLabels[simulation.status]}
                color={statusColor(simulation.status)}
              />

              <HelpTooltip label="About the shared experiment">
                All connected browsers view and control the same experiment.
                Starting or stopping it affects every connected browser.
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

            <Stack direction="row" spacing={0.5} alignItems="center">
              <Button
                variant="contained"
                disabled={!canStop}
                onClick={onStopAndSave}
              >
                Stop and save to Results
              </Button>

              <HelpTooltip label="About stopping with a result">
                Stop this run and keep its observed metrics, including unfinished
                transfers. Ordinary Stop and clear does not save a result.
              </HelpTooltip>
            </Stack>
          </Stack>
        )}

        {visibleError && (
          <Alert severity="error">
            {visibleError}
          </Alert>
        )}

        {children}
      </Stack>
    </Box>
  );
}

export default ControlPanel;
