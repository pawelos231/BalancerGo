import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Button,
  Card,
  CardContent,
  CardHeader,
  FormControl,
  InputAdornment,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
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

// These definitions describe inputs; the backend supplies their default values.
const mainFields: Field[] = [
  {
    name: 'clients',
    label: 'TCP clients',
    integer: true,
    min: 0,
    max: 2 ** 24,
    exclusiveMax: true,
    help:
      'Number of clients scheduled for this run using the simplified TCP model. ' +
      'Each client sends one finite stream through the load balancer, then closes its connection.',
  },
  {
    name: 'servers',
    label: 'Backend servers',
    integer: true,
    min: 0,
    max: 10000,
    help:
      'Number of simulated backend servers available to the load balancer. ' +
      'Set 0 to examine a run with no available backend.',
  },
  {
    name: 'packets',
    label: 'Segments per client',
    integer: true,
    min: 0,
    hint: 'Used when payload bytes per client = -1.',
    help:
      'Number of 1,024-byte payload segments per client when payload bytes per client is -1. ' +
      'A client sends one segment at a time and waits for its acknowledgement before sending the next.',
  },
  {
    name: 'durationSeconds',
    label: 'Virtual duration (s)',
    min: 0,
    help:
      'Hard end time on the virtual simulation clock. ' +
      'The run stops at this limit even if some streams are incomplete. ' +
      'The real time needed to reach it depends on playback speed and your computer.',
  },
  {
    name: 'speed',
    label: 'Playback speed (virtual s / real s)',
    min: 0,
    hint: '0 = no waiting; 1 = real time; 2 = twice real time.',
    help:
      'Target pace of virtual time compared with real time. ' +
      'Use 0 to remove deliberate waits, 1 for real-time pacing, or 2 for two virtual seconds per real second. ' +
      'Heavy workloads may run slower than the selected pace.',
  },
];

const detailFields: Field[] = [
  {
    name: 'bytes',
    label: 'Payload bytes per client',
    integer: true,
    min: -1,
    hint: '-1 uses the segment count; 0 or more sets the exact payload size.',
    help:
      'Set -1 to use the segment count multiplied by 1,024 payload bytes per client. ' +
      'Any nonnegative value overrides that count with an exact payload size; 0 sends no data. ' +
      'The final segment can be shorter than 1,024 bytes.',
  },
  {
    name: 'clientGapMs',
    label: 'Client creation interval (virtual ms)',
    min: 0,
    help:
      'Virtual delay between creating successive clients. ' +
      'Set 0 to schedule all clients at the same virtual time; ' +
      'their shared network links still transmit packets one at a time.',
  },
  {
    name: 'packetGapMs',
    label: 'Pause between segments (virtual ms)',
    min: 0,
    help:
      'Virtual pause before each new data segment: after the handshake for the first segment, ' +
      'then after each acknowledgement. Since each client waits for an acknowledgement, ' +
      'the actual interval between segments also includes the packet round trip.',
  },
  {
    name: 'snapshotIntervalMs',
    label: 'Snapshot interval (virtual ms)',
    min: 0,
    exclusiveMin: true,
    help:
      'Virtual time between state updates: 100 ms gives about 10 updates per real second at speed 1, ' +
      'or 20 at speed 2. Fast runs may skip intermediate updates. ' +
      'This also sets how often commands from the page are applied; ' +
      'simulated events still happen at their exact times.',
  },
  {
    name: 'bandwidthBitsPerSecond',
    label: 'Link bandwidth (bit/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'Maximum bitrate of each access link and each link leaving the LB, including modeled IPv4 and TCP headers. ' +
      'One gigabit per second is 1,000,000,000 bit/s, or 125 MB/s of raw traffic. ' +
      'Client packets and backend replies first reach the upstream aggregation point through access links, ' +
      'then share the separately configured LB ingress link. ' +
      'Links leaving the LB also share its independent forwarding budget.',
  },
  {
    name: 'forwardingBitsPerSecond',
    label: 'LB forwarding budget (bits/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'One shared bitrate limit for all packets leaving the load balancer, ' +
      'both toward backend servers and toward clients. ' +
      'Each packet is counted once when its outgoing transmission starts, ' +
      'including modeled headers, control packets and retransmissions. ' +
      'Packets entering the load balancer are not counted against this budget.',
  },
  {
    name: 'packetTravelTimeMs',
    label: 'Packet travel time (virtual ms)',
    min: 0,
    help:
      'Virtual travel time after transmission on each access link and each link leaving the LB. ' +
      'On a link with 1 ms of travel time, a packet finishing transmission at 5 ms arrives at 6 ms. ' +
      'The link can transmit the next packet while the previous one is still travelling. ' +
      'The common ingress link has a separate travel time setting.',
  },
  {
    name: 'linkQueue',
    label: 'Link queue slots (packets)',
    integer: true,
    min: 0,
    help:
      'Waiting packet slots on each access link and outgoing LB path, excluding active transmissions ' +
      'and packets already travelling. LB output packets may wait for their link ' +
      'or the shared forwarding budget. New packets are dropped when the waiting slots are full; ' +
      '0 admits a packet only when it can start transmitting immediately. ' +
      'The common ingress FIFO has its own queue capacity setting.',
  },
  {
    name: 'loss',
    label: 'Packet loss probability (0–1)',
    min: 0,
    max: 1,
    help:
      'Independent probability of losing a packet on each access link or outgoing LB link after transmission; ' +
      '0.1 means a 10% chance on each link. Use 0 for no random loss or 1 to lose every packet. ' +
      'Lost packets still consume link bandwidth and can trigger client retries or timeouts. ' +
      'The common ingress link has a separate loss probability setting.',
  },
  {
    name: 'ingressBandwidthBitsPerSecond',
    label: 'LB ingress bandwidth (bit/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'Bitrate of the single physical path from upstream aggregation into the load balancer. ' +
      'Packets from all clients and replies from all backends share this limit, ' +
      'including modeled headers, acknowledgements and retransmissions. ' +
      'Access links and this ingress link serialize packets on separate physical sections. ' +
      'This setting is independent of the LB forwarding budget.',
  },
  {
    name: 'ingressPacketTravelTimeMs',
    label: 'LB ingress packet travel time (virtual ms)',
    min: 0,
    help:
      'Virtual travel time on the shared upstream-to-LB link after its transmission finishes. ' +
      'It applies to both client packets and backend replies and is added to travel time on their access links. ' +
      'The LB receives the packet only after it completes both physical sections.',
  },
  {
    name: 'ingressQueueCapacity',
    label: 'LB ingress queue slots (packets)',
    integer: true,
    min: 0,
    help:
      'Packet slots in the shared upstream FIFO before the LB receives a packet. ' +
      'Client packets and backend replies compete for the same waiting slots. ' +
      'Active transmissions and packets already travelling do not occupy waiting slots. ' +
      'With 0 slots, a packet is admitted only if transmission can start immediately; otherwise it is dropped. ' +
      'This is a physical network queue, not a kernel receive queue.',
  },
  {
    name: 'ingressLoss',
    label: 'LB ingress loss probability',
    min: 0,
    max: 1,
    help:
      'Probability of losing a packet after transmission on the shared upstream-to-LB link. ' +
      'It applies to client packets and backend replies, including acknowledgements and retransmissions. ' +
      '0 means no random loss; 1 loses every packet on this link. ' +
      'A lost packet still consumes ingress bandwidth and can trigger a retry or timeout.',
  },
  {
    name: 'seed',
    label: 'Random seed',
    integer: true,
    help:
      'Starting value for the simulation random generators. ' +
      'The same configuration and seed reproduce the random choices ' +
      'when commands are also applied at the same virtual times.',
  },
  {
    name: 'maxEvents',
    label: 'Event execution limit',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'Maximum number of simulated events, such as packet arrivals, timer expirations, ' +
      'health checks and state updates. The run stops with an error if it reaches this limit. ' +
      'Increase it for larger experiments.',
  },
];

const numericFields = [...mainFields, ...detailFields];

const algorithmHelp =
  'Policy used to choose a backend for new connections at the start of the run. ' +
  'Round Robin rotates through eligible backends; Random chooses using the seeded random generator. ' +
  'Changing the policy during a run affects new connections while existing ones keep their backend.';

function fieldError(field: Field, raw: string): string | undefined {
  if (raw.trim() === '' || !Number.isFinite(Number(raw))) {
    return 'Enter a finite number.';
  }

  const value = Number(raw);

  if (field.integer && !Number.isSafeInteger(value)) {
    return 'Enter an integer between -(2^53 - 1) and 2^53 - 1.';
  }

  const belowMinimum = field.min !== undefined && (
    value < field.min || (field.exclusiveMin && value === field.min)
  );

  if (belowMinimum) {
    const comparison = field.exclusiveMin ? 'greater than' : 'at least';
    return `Value must be ${comparison} ${field.min}.`;
  }

  const aboveMaximum = field.max !== undefined && (
    value > field.max || (field.exclusiveMax && value === field.max)
  );

  if (aboveMaximum) {
    const comparison = field.exclusiveMax ? 'less than' : 'at most';
    return `Value must be ${comparison} ${field.max}.`;
  }
}

function createDraft(config: SimulationConfig): Draft {
  const entries = Object.entries(config).map(([name, value]) => [name, String(value)]);
  return Object.fromEntries(entries) as Draft;
}

function createStartConfig(draft: Draft): SimulationConfig {
  const entries = numericFields.map((field) => [field.name, Number(draft[field.name])]);
  const config = Object.fromEntries(entries) as Omit<SimulationConfig, 'algorithm'>;

  return { ...config, algorithm: draft.algorithm as AlgorithmName };
}

interface SimulationFormProps {
  initialConfig: SimulationConfig;
  connected: boolean;
  running: boolean;
  pending: boolean;
  onStart: (config: SimulationConfig) => boolean;
}

function SimulationForm({
  initialConfig,
  connected,
  running,
  pending,
  onStart,
}: SimulationFormProps) {
  // Keep unsent edits when live snapshots update the initial configuration.
  const [draft, setDraft] = useState<Draft>(() => createDraft(initialConfig));

  const validNumbers = numericFields.every((field) => !fieldError(field, draft[field.name]));
  const validAlgorithm = draft.algorithm === 'roundRobin' || draft.algorithm === 'random';
  const canStart = connected && !running && !pending && validNumbers && validAlgorithm;
  const fieldsDisabled = !connected || pending;

  const startLabel = pending ? 'Waiting for confirmation…' : 'Start simulation';
  const nextRunHint = running
    ? 'You can prepare the parameters for the next run.'
    : 'You can start another experiment after this run finishes.';

  function updateDraft(name: keyof SimulationConfig, value: string) {
    setDraft((previous) => ({ ...previous, [name]: value }));
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (!canStart) {
      return;
    }

    onStart(createStartConfig(draft));
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
          disabled={fieldsDisabled}
          onChange={(event) => updateDraft(field.name, event.target.value)}
          inputProps={{
            min: field.min,
            max: field.max,
            step: field.integer ? 1 : 'any',
          }}
          slotProps={{
            input: {
              endAdornment: (
                <InputAdornment position="end">
                  <HelpTooltip label={field.label}>
                    {field.help}
                  </HelpTooltip>
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
      <CardHeader
        title="Start simulation"
        subheader="Set the experiment parameters, then start the run."
      />

      <CardContent>
        <Stack spacing={2}>
          <Grid container spacing={2}>
            {mainFields.map(renderField)}

            <Grid size={{ xs: 12, sm: 6, md: 4 }}>
              <FormControl fullWidth size="small" disabled={fieldsDisabled}>
                <InputLabel id="start-alg-label">
                  Initial algorithm
                </InputLabel>

                <Select
                  labelId="start-alg-label"
                  label="Initial algorithm"
                  value={draft.algorithm}
                  onChange={(event) => updateDraft('algorithm', event.target.value)}
                  endAdornment={(
                    <InputAdornment position="end" sx={{ mr: 3 }}>
                      <HelpTooltip label="Initial algorithm">
                        {algorithmHelp}
                      </HelpTooltip>
                    </InputAdornment>
                  )}
                >
                  <MenuItem value="roundRobin">Round Robin</MenuItem>
                  <MenuItem value="random">Random</MenuItem>
                </Select>
              </FormControl>
            </Grid>
          </Grid>

          <Accordion
            disableGutters
            elevation={0}
            sx={{ border: '1px solid', borderColor: 'divider' }}
          >
            <AccordionSummary expandIcon={<ExpandMoreIcon />}>
              <Typography>Transport, links and repeatability</Typography>
            </AccordionSummary>

            <AccordionDetails>
              <Grid container spacing={2}>
                {detailFields.map(renderField)}
              </Grid>
            </AccordionDetails>
          </Accordion>

          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={2}
            alignItems={{ sm: 'center' }}
          >
            <Button type="submit" variant="contained" disabled={!canStart}>
              {startLabel}
            </Button>

            <Typography variant="body2" color="text.secondary">
              {nextRunHint}
            </Typography>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default SimulationForm;
