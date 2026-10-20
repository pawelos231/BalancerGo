import {
  Accordion, AccordionDetails, AccordionSummary, Button, Card, CardContent,
  CardHeader, FormControl, InputAdornment, InputLabel, MenuItem, Select, Stack, TextField, Typography,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useState } from 'react';
import type { FormEvent } from 'react';
import type { AlgorithmName, SimulationConfig } from '../types';
import HelpTooltip from './HelpTooltip';

type NumericField = Exclude<keyof SimulationConfig, 'algorithm'>;
type Draft = Record<keyof SimulationConfig, string>;

interface Field {
  name: NumericField;
  label: string;
  help: string;
  integer?: boolean;
  min?: number;
  max?: number;
  exclusiveMin?: boolean;
  exclusiveMax?: boolean;
  hint?: string;
}

// Validation constraints and labels only: defaults come from the backend.
const mainFields: Field[] = [
  {
    name: 'clients', label: 'TCP clients', integer: true, min: 0, max: 2 ** 24, exclusiveMax: true,
    help: 'Number of clients scheduled for this run using the simplified TCP model. Each client sends one finite stream through the load balancer, then closes its connection.',
  },
  {
    name: 'servers', label: 'Backend servers', integer: true, min: 0, max: 10000,
    help: 'Number of simulated backend servers available to the load balancer. Set 0 to examine a run with no available backend.',
  },
  {
    name: 'packets', label: 'Segments per client', integer: true, min: 0,
    hint: 'Used when payload bytes per client = -1.',
    help: 'Number of 1,024-byte payload segments per client when payload bytes per client is -1. A client sends one segment at a time and waits for its acknowledgement before sending the next.',
  },
  {
    name: 'durationSeconds', label: 'Virtual duration (s)', min: 0,
    help: 'Hard end time on the virtual simulation clock. The run stops at this limit even if some streams are incomplete. The real time needed to reach it depends on playback speed and your computer.',
  },
  {
    name: 'speed', label: 'Playback speed (virtual s / real s)', min: 0,
    hint: '0 = no waiting; 1 = real time; 2 = twice real time.',
    help: 'Target pace of virtual time compared with real time. Use 0 to remove deliberate waits, 1 for real-time pacing, or 2 for two virtual seconds per real second. Heavy workloads may run slower than the selected pace.',
  },
];

const detailFields: Field[] = [
  {
    name: 'bytes', label: 'Payload bytes per client', integer: true, min: -1,
    hint: '-1 uses the segment count; 0 or more sets the exact payload size.',
    help: 'Set -1 to use the segment count multiplied by 1,024 payload bytes per client. Any nonnegative value overrides that count with an exact payload size; 0 sends no data. The final segment can be shorter than 1,024 bytes.',
  },
  {
    name: 'clientGapMs', label: 'Client creation interval (virtual ms)', min: 0,
    help: 'Virtual delay between creating successive clients. Set 0 to schedule all clients at the same virtual time; their shared network links still transmit packets one at a time.',
  },
  {
    name: 'packetGapMs', label: 'Pause between segments (virtual ms)', min: 0,
    help: 'Virtual pause before each new data segment: after the handshake for the first segment, then after each acknowledgement. Since each client waits for an acknowledgement, the actual interval between segments also includes the packet round trip.',
  },
  {
    name: 'snapshotIntervalMs', label: 'Snapshot interval (virtual ms)', min: 0, exclusiveMin: true,
    help: 'Virtual time between state updates: 100 ms gives about 10 updates per real second at speed 1, or 20 at speed 2. Fast runs may skip intermediate updates. This also sets how often commands from the page are applied; simulated events still happen at their exact times.',
  },
  {
    name: 'bandwidthBitsPerSecond', label: 'Link bandwidth (bit/s)', integer: true, min: 0, exclusiveMin: true,
    help: 'Maximum bitrate of each directed link, including the modeled IPv4 and TCP headers. One gigabit per second is 1,000,000,000 bit/s, or 125 MB/s of raw traffic. Clients share their links to and from the load balancer, while each backend has its own pair of links.',
  },
  {
    name: 'linkDelayMs', label: 'Link propagation delay (virtual ms)', min: 0,
    help: 'Virtual travel time added after each packet finishes transmitting. With a 1 ms delay, a packet finishing transmission at 5 ms arrives at 6 ms. The link can transmit the next packet while the previous one is still travelling.',
  },
  {
    name: 'linkQueue', label: 'Link queue slots (packets)', integer: true, min: 0,
    help: 'Number of packet slots waiting for a busy link, excluding the packet currently transmitting and packets already travelling. New packets are dropped when the waiting slots are full; 0 allows one active transmission with no waiting queue.',
  },
  {
    name: 'loss', label: 'Packet loss probability (0–1)', min: 0, max: 1,
    help: 'Independent probability of losing a packet on each link after transmission; 0.1 means a 10% chance on each link. Use 0 for no random loss or 1 to lose every packet. Lost packets still consume link bandwidth and can trigger client retries or timeouts.',
  },
  {
    name: 'seed', label: 'Random seed', integer: true,
    help: 'Starting value for the simulation random generators. The same configuration and seed reproduce the random choices when commands are also applied at the same virtual times.',
  },
  {
    name: 'maxEvents', label: 'Event execution limit', integer: true, min: 0, exclusiveMin: true,
    help: 'Maximum number of simulated events, such as packet arrivals, timer expirations, health checks and state updates. The run stops with an error if it reaches this limit. Increase it for larger experiments.',
  },
];

const algorithmHelp = 'Policy used to choose a backend for new connections at the start of the run. Round Robin rotates through eligible backends; Random chooses using the seeded random generator. Changing the policy during a run affects new connections while existing ones keep their backend.';

function fieldError(field: Field, raw: string): string | undefined {
  if (raw.trim() === '' || !Number.isFinite(Number(raw))) return 'Enter a finite number.';
  const value = Number(raw);
  if (field.integer && !Number.isSafeInteger(value)) return 'Enter an integer between -(2^53 - 1) and 2^53 - 1.';
  if (field.min !== undefined && (value < field.min || (field.exclusiveMin && value === field.min))) {
    return `Value must be ${field.exclusiveMin ? 'greater than' : 'at least'} ${field.min}.`;
  }
  if (field.max !== undefined && (value > field.max || (field.exclusiveMax && value === field.max))) {
    return `Value must be ${field.exclusiveMax ? 'less than' : 'at most'} ${field.max}.`;
  }
}

interface SimulationFormProps {
  initialConfig: SimulationConfig;
  connected: boolean;
  running: boolean;
  pending: boolean;
  onStart: (config: SimulationConfig) => boolean;
}

function SimulationForm({ initialConfig, connected, running, pending, onStart }: SimulationFormProps) {
  // Preserve the draft across snapshots; reconnect remounts the form with the
  // first fresh snapshot, rather than resetting edits on every running frame.
  const [draft, setDraft] = useState<Draft>(() => Object.fromEntries(
    Object.entries(initialConfig).map(([name, value]) => [name, String(value)]),
  ) as Draft);
  const fields = [...mainFields, ...detailFields];
  const valid = fields.every((field) => !fieldError(field, draft[field.name]))
    && (draft.algorithm === 'roundRobin' || draft.algorithm === 'random');

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!connected || running || pending || !valid) return;
    const config = Object.fromEntries(fields.map((field) => [field.name, Number(draft[field.name])])) as Omit<SimulationConfig, 'algorithm'>;
    onStart({ ...config, algorithm: draft.algorithm as AlgorithmName });
  }

  function renderField(field: Field) {
    const error = fieldError(field, draft[field.name]);
    return (
      <Grid size={{ xs: 12, sm: 6, md: 4 }} key={field.name}>
        <TextField
          fullWidth
          size="small"
          type="number"
          label={field.label}
          value={draft[field.name]}
          disabled={!connected || pending}
          onChange={(event) => setDraft((previous) => ({ ...previous, [field.name]: event.target.value }))}
          inputProps={{ min: field.min, max: field.max, step: field.integer ? 1 : 'any' }}
          slotProps={{
            input: {
              endAdornment: (
                <InputAdornment position="end">
                  <HelpTooltip label={field.label}>{field.help}</HelpTooltip>
                </InputAdornment>
              ),
            },
          }}
          error={Boolean(error)}
          helperText={error ?? field.hint}
        />
      </Grid>
    );
  }

  return (
    <Card component="form" variant="outlined" onSubmit={submit} noValidate>
      <CardHeader title="Start simulation" subheader="Set the experiment parameters, then start the run." />
      <CardContent>
        <Stack spacing={2}>
          <Grid container spacing={2}>
            {mainFields.map(renderField)}
            <Grid size={{ xs: 12, sm: 6, md: 4 }}>
              <FormControl fullWidth size="small" disabled={!connected || pending}>
                <InputLabel id="start-alg-label">Initial algorithm</InputLabel>
                <Select
                  labelId="start-alg-label"
                  label="Initial algorithm"
                  value={draft.algorithm}
                  onChange={(event) => setDraft((previous) => ({ ...previous, algorithm: event.target.value }))}
                  endAdornment={(
                    <InputAdornment position="end" sx={{ mr: 3 }}>
                      <HelpTooltip label="Initial algorithm">{algorithmHelp}</HelpTooltip>
                    </InputAdornment>
                  )}
                >
                  <MenuItem value="roundRobin">Round Robin</MenuItem>
                  <MenuItem value="random">Random</MenuItem>
                </Select>
              </FormControl>
            </Grid>
          </Grid>

          <Accordion disableGutters elevation={0} sx={{ border: '1px solid', borderColor: 'divider' }}>
            <AccordionSummary expandIcon={<ExpandMoreIcon />}>
              <Typography>Transport, links and repeatability</Typography>
            </AccordionSummary>
            <AccordionDetails>
              <Grid container spacing={2}>{detailFields.map(renderField)}</Grid>
            </AccordionDetails>
          </Accordion>

          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} alignItems={{ sm: 'center' }}>
            <Button type="submit" variant="contained" disabled={!connected || running || pending || !valid}>
              {pending ? 'Waiting for confirmation…' : 'Start simulation'}
            </Button>
            <Typography variant="body2" color="text.secondary">
              {running ? 'You can prepare the parameters for the next run.' : 'You can start another experiment after this run finishes.'}
            </Typography>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default SimulationForm;
