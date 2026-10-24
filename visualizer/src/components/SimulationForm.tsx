import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  FormControl,
  FormHelperText,
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
import type { FormEvent } from 'react';
import type { ScenarioDraft, ScenarioField } from '../scenarios/draft';
import type { Scenario } from '../scenarios/types';
import HelpTooltip from './HelpTooltip';

type NumericField = Exclude<ScenarioField, 'algorithm'>;

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
    label: 'TCP clients (total)',
    integer: true,
    min: 0,
    max: 2 ** 24,
    exclusiveMax: true,
    help:
      'Number of clients scheduled for this run using the simplified TCP model. ' +
      'Changing the total redistributes clients proportionally across existing groups. ' +
      'A change that would remove a client with explicit overrides is rejected.',
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
    label: 'Default segments per client',
    integer: true,
    min: 0,
    hint: 'Used when default payload bytes per client = -1.',
    help:
      'Number of 1,024-byte payload segments per client when payload bytes per client is -1. ' +
      'Explicit stream overrides on groups and clients stay unchanged. ' +
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
    label: 'Default payload bytes per client',
    integer: true,
    min: -1,
    hint: '-1 uses the segment count; 0 or more sets the exact payload size.',
    help:
      'Set -1 to use the segment count multiplied by 1,024 payload bytes per client. ' +
      'Any nonnegative value overrides that count with an exact payload size; 0 sends no data. ' +
      'The final segment can be shorter than 1,024 bytes. Group and client overrides stay unchanged.',
  },
  {
    name: 'clientGapMs',
    label: 'Client creation interval (all groups, virtual ms)',
    min: 0,
    help:
      'Virtual delay between creating successive clients in each group. Editing this field applies to all groups. ' +
      'Set 0 to schedule all clients at the same virtual time; ' +
      'each has its own links, while shared LB ingress and forwarding limits still apply.',
  },
  {
    name: 'packetGapMs',
    label: 'Default pause between segments (virtual ms)',
    min: 0,
    help:
      'Virtual pause before each new data segment: after the handshake for the first segment, ' +
      'then after each acknowledgement. Since each client waits for an acknowledgement, ' +
      'the actual interval between segments also includes the packet round trip. ' +
      'Explicit group and client stream overrides stay unchanged.',
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
    label: 'Default link bandwidth (bit/s)',
    integer: true,
    min: 0,
    exclusiveMin: true,
    help:
      'Default maximum bitrate of each client or backend link, including modeled IPv4 and TCP headers. ' +
      'Profiles and directional overrides retain their configured values. ' +
      'One gigabit per second is 1,000,000,000 bit/s, or 125 MB/s of raw traffic. ' +
      'Each client and backend has its own links. Incoming packets join their side\'s LB input queue, ' +
      'then use one shared LB input bandwidth limit. ' +
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
    label: 'Default packet travel time (virtual ms)',
    min: 0,
    help:
      'Default virtual travel time after transmission on each client or backend link. ' +
      'On a link with 1 ms of travel time, a packet finishing transmission at 5 ms arrives at 6 ms. ' +
      'The link can transmit the next packet while the previous one is still travelling. ' +
      'The common ingress link has a separate travel time setting. Profiles and overrides stay unchanged.',
  },
  {
    name: 'linkQueue',
    label: 'Default link queue slots (packets)',
    integer: true,
    min: 0,
    help:
      'Waiting packet slots on each client or backend access link and outgoing LB path, excluding active transmissions ' +
      'and packets already travelling. LB output packets may wait for their link ' +
      'or the shared forwarding budget. New packets are dropped when the waiting slots are full; ' +
      '0 admits a packet only when it can start transmitting immediately. ' +
      'The client-side and backend-side LB input queues have a separate capacity setting.',
  },
  {
    name: 'loss',
    label: 'Default packet loss probability (0–1)',
    min: 0,
    max: 1,
    help:
      'Independent probability of losing a packet on each client or backend link after transmission; ' +
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
      'One shared input bitrate for the client-side and backend-side queues. ' +
      'Packets from all clients and replies from all backends share this limit, ' +
      'including modeled headers, acknowledgements and retransmissions. ' +
      'Access-link transmission finishes before a packet joins its LB input queue. ' +
      'This setting is independent of the LB forwarding budget.',
  },
  {
    name: 'ingressPacketTravelTimeMs',
    label: 'LB ingress packet travel time (virtual ms)',
    min: 0,
    help:
      'Extra virtual travel time after a packet finishes transmission through the shared LB input. ' +
      'It applies to both client packets and backend replies and is added to travel time on their access links. ' +
      'The LB receives the packet only after it completes both physical sections.',
  },
  {
    name: 'ingressQueueCapacity',
    label: 'LB input slots per queue (packets)',
    integer: true,
    min: 0,
    help:
      'Waiting slots in each of the two LB input queues: one for clients and one for backends. ' +
      'A value of 1024 gives each queue 1024 slots; they still share one input bitrate limit. ' +
      'The oldest waiting packet across both queues uses the shared receiver next. ' +
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
      'Probability of losing a packet after transmission through the shared LB input. ' +
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

const algorithmHelp =
  'Policy used to choose a backend for new connections at the start of the run. ' +
  'Round Robin rotates through eligible backends; Random chooses using the seeded random generator. ' +
  'Changing the policy during a run affects new connections while existing ones keep their backend.';

interface SimulationFormProps {
  scenario: Scenario;
  values: ScenarioDraft;
  errors: Partial<Record<ScenarioField, string>>;
  connected: boolean;
  running: boolean;
  pending: boolean;
  validating: boolean;
  onChange: (name: ScenarioField, raw: string) => void;
  onFocus: (name: ScenarioField) => void;
  onBlur: (name: ScenarioField) => void;
  onStart: () => void;
}

function SimulationForm({
  scenario,
  values,
  errors,
  connected,
  running,
  pending,
  validating,
  onChange,
  onFocus,
  onBlur,
  onStart,
}: SimulationFormProps) {
  const hasErrors = Object.keys(errors).length > 0;
  const canStart = connected && !running && !pending && !validating && !hasErrors;
  const fieldsDisabled = !connected || pending || validating;
  const groups = scenario.workload.clientGroups;
  const displayedGroups = groups.slice(0, 12);
  const profiles = scenario.network.profiles ?? [];

  const startLabel = pending ? 'Waiting for confirmation…' : 'Start simulation';
  const nextRunHint = running
    ? 'You can prepare the parameters for the next run.'
    : 'Start uses the settings shown above.';

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (!canStart) {
      return;
    }

    onStart();
  }

  function renderField(field: Field) {
    const error = errors[field.name];
    const mixed = field.name === 'clientGapMs' && values[field.name] === '' && !error;
    const hint = mixed ? 'Mixed group intervals. Editing applies to every group.' : field.hint;

    return (
      <Grid size={{ xs: 12, sm: 6, md: 4 }} key={field.name}>
        <TextField
          fullWidth
          size="small"
          type="number"
          label={field.label}
          value={values[field.name]}
          placeholder={mixed ? 'Mixed' : undefined}
          disabled={fieldsDisabled}
          onChange={(event) => onChange(field.name, event.target.value)}
          onFocus={() => onFocus(field.name)}
          onBlur={() => onBlur(field.name)}
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
          helperText={error ?? hint}
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
          <Alert severity="info">
            {groups.length} client groups and {profiles.length} link profiles are retained.
            Stream and link fields edit defaults; group, profile and client overrides stay in place.
          </Alert>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            {displayedGroups.map((group) => (
              <Chip key={group.id} label={`${group.id}: ${group.count} clients`} size="small" />
            ))}
            {displayedGroups.length < groups.length && (
              <Typography variant="caption" color="text.secondary">
                {groups.length - displayedGroups.length} more groups are retained in the scenario.
              </Typography>
            )}
          </Stack>
          {groups.length > 1 && (
            <Typography variant="body2" color="text.secondary">
              Changing the total redistributes clients proportionally across these groups.
            </Typography>
          )}
          <Grid container spacing={2}>
            {mainFields.map(renderField)}

            <Grid size={{ xs: 12, sm: 6, md: 4 }}>
              <FormControl
                fullWidth
                size="small"
                disabled={fieldsDisabled}
                error={Boolean(errors.algorithm)}
              >
                <InputLabel id="start-alg-label">
                  Initial algorithm
                </InputLabel>

                <Select
                  labelId="start-alg-label"
                  label="Initial algorithm"
                  value={values.algorithm}
                  onChange={(event) => onChange('algorithm', event.target.value)}
                  onFocus={() => onFocus('algorithm')}
                  onBlur={() => onBlur('algorithm')}
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
                {errors.algorithm && <FormHelperText>{errors.algorithm}</FormHelperText>}
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

          {hasErrors && (
            <Alert severity="error">
              Correct the highlighted settings before starting.
            </Alert>
          )}

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
