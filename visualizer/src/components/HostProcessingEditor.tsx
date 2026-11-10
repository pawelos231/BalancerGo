import { useEffect, useRef, useState } from 'react';
import {
  Accordion, AccordionDetails, AccordionSummary, Alert, Button, Chip,
  FormControl, InputLabel, MenuItem, Select, Stack, TextField, Typography,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';

import type { Scenario } from '../scenarios/types';
import { selectedScenarioState } from '../scenarios/draft';
import {
  backendHostTarget, commonBackendHostValues, hostErrorKey, hostFields,
  hostValues, scenarioHost, validateHostValue,
} from '../scenarios/host';
import type { HostEdits, HostField, HostTarget, HostValues } from '../scenarios/host';

interface HostProcessingEditorProps {
  scenario: Scenario;
  selectedAt: string;
  edits: HostEdits;
  disabled: boolean;
  onChange: (target: HostTarget, field: HostField, raw: string) => void;
  onApplyAll: (values: HostValues) => string | undefined;
  onStagedChange: (staged: boolean) => void;
}

const labels: Record<HostField, string> = {
  cores: 'Virtual cores',
  packetProcessingTimeUs: 'Packet processing time (virtual µs)',
  queueCapacity: 'CPU queue waiting slots',
};
const help: Record<HostField, string> = {
  cores: 'Packets that can be processed at once. These are modeled resources, not Go goroutines.',
  packetProcessingTimeUs: 'Time one packet occupies a core. Zero disables CPU limits; link and TCP limits still apply.',
  queueCapacity: 'Packets waiting for a core, excluding packets already processing. Zero accepts only when a core is free.',
};

// Shared controls apply only on the explicit button; individual edits stay per backend.
export default function HostProcessingEditor({
  scenario, selectedAt, edits, disabled, onChange, onApplyAll, onStagedChange,
}: HostProcessingEditorProps) {
  const backends = selectedScenarioState(scenario, selectedAt).resources.backends;
  const [selected, setSelected] = useState(backends[0]?.id ?? '');
  const [shared, setShared] = useState<Partial<HostValues>>({});
  const sharedRef = useRef<Partial<HostValues>>({});
  const [sharedError, setSharedError] = useState<string | null>(null);
  const selectedID = backends.some(backend => backend.id === selected) ? selected : backends[0]?.id ?? '';
  const common = commonBackendHostValues(scenario, selectedAt);
  const sharedValues = { ...common, ...shared };
  const hasStagedValues = hostFields.some(field => (
    shared[field] !== undefined && shared[field] !== common[field]
  ));

  useEffect(() => {
    onStagedChange(hasStagedValues);
  }, [hasStagedValues, onStagedChange]);

  function changeShared(field: HostField, raw: string) {
    const next = { ...sharedRef.current, [field]: raw };
    sharedRef.current = next;
    setShared(next);
    setSharedError(null);
    // Block other actions in this event, before React renders the pending input.
    onStagedChange(hostFields.some(name => (
      next[name] !== undefined && next[name] !== common[name]
    )));
  }

  function applyShared() {
    const failure = onApplyAll(sharedValues);
    if (failure) {
      setSharedError(failure);
      return;
    }
    sharedRef.current = {};
    setShared({});
    setSharedError(null);
    onStagedChange(false);
  }
  const different = backends.filter(backend => {
    const current = hostValues(backend.host);
    const first = hostValues(backends[0]?.host);
    return hostFields.some(field => current[field] !== first[field]);
  }).length;
  const errorCount = Object.keys(edits.errors).length;
  const sharedInvalid = hostFields.some(field => validateHostValue(field, sharedValues[field]));
  const commonHint = different > 0
    ? `${different} backends differ from the first backend. Blank fields indicate mixed values.`
    : 'Backends currently share the same effective settings.';

  function fields(target: HostTarget, title: string) {
    const values = { ...hostValues(scenarioHost(scenario, target, selectedAt)), ...edits.values[target] };
    const active = Number(values.packetProcessingTimeUs) > 0;
    const rate = active ? Number(values.cores) * 1_000_000 / Number(values.packetProcessingTimeUs) : 0;
    const invalid = hostFields.some(field => validateHostValue(field, values[field]));
    let capacity = 'CPU model disabled.';
    if (invalid) {
      capacity = 'Correct the host settings to calculate its service limit.';
    } else if (active) {
      capacity = `Theoretical service limit: ${rate.toLocaleString()} packets / virtual s.`;
    }

    return (
      <Stack spacing={1}>
        <Typography variant="subtitle2">{title}</Typography>
        <Typography variant="body2" color="text.secondary">
          {capacity}
          {target === 'loadBalancer' && ' Client and backend traffic share this CPU pool.'}
        </Typography>
        <Grid container spacing={2}>
          {hostFields.map(field => {
            const error = edits.errors[hostErrorKey(target, field)];
            return (
              <Grid size={{ xs: 12, sm: 6, md: 4 }} key={field}>
                <TextField
                  fullWidth size="small" type="number"
                  label={`${title}: ${labels[field]}`}
                  value={values[field]}
                  disabled={disabled || (!active && field !== 'packetProcessingTimeUs' && !error)}
                  error={Boolean(error)} helperText={error ?? help[field]}
                  inputProps={{ min: field === 'cores' ? 1 : 0, step: field === 'packetProcessingTimeUs' ? 0.001 : 1 }}
                  onChange={event => onChange(target, field, event.target.value)}
                />
              </Grid>
            );
          })}
        </Grid>
      </Stack>
    );
  }

  return (
    <Accordion disableGutters elevation={0} sx={{ border: '1px solid', borderColor: 'divider' }}>
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Stack spacing={0.5}>
          <Stack direction="row" spacing={1} alignItems="center">
            <Typography variant="subtitle1">Host processing</Typography>
            {errorCount > 0 && <Chip size="small" color="error" label={`${errorCount} invalid`} />}
          </Stack>
          <Typography variant="body2" color="text.secondary">
            Edit CPU processing in the selected state independently from network bandwidth. Set packet time to zero to disable it.
          </Typography>
        </Stack>
      </AccordionSummary>
      <AccordionDetails>
        <Stack spacing={3}>
          {fields('loadBalancer', 'Load balancer')}

          <Stack spacing={1}>
            <Typography variant="subtitle2">Apply common settings to backends</Typography>
            <Typography variant="body2" color="text.secondary">
              {commonHint}
              {' '}These controls take effect only when you press Apply to all backends.
            </Typography>
            <Grid container spacing={2}>
              {hostFields.map(field => {
                const error = shared[field] !== undefined
                  ? validateHostValue(field, sharedValues[field]) : undefined;
                return (
                  <Grid size={{ xs: 12, sm: 6, md: 4 }} key={field}>
                    <TextField
                      fullWidth size="small" type="number"
                      label={`All backends: ${labels[field]}`}
                      value={sharedValues[field]}
                      placeholder="Mixed"
                      disabled={disabled || backends.length === 0}
                      error={Boolean(error)} helperText={error ?? help[field]}
                      onChange={event => changeShared(field, event.target.value)}
                    />
                  </Grid>
                );
              })}
            </Grid>
            <Button
              variant="outlined" sx={{ alignSelf: 'flex-start' }}
              disabled={disabled || backends.length === 0 || sharedInvalid}
              onClick={applyShared}
            >Apply to all backends</Button>
            {sharedError && <Alert severity="error">{sharedError}</Alert>}
            {hasStagedValues && (
              <Alert severity="warning">
                Common backend CPU inputs have not been applied. Apply them or use
                Discard unapplied form inputs before Start or file actions.
              </Alert>
            )}
          </Stack>

          {backends.length > 0 ? (
            <Stack spacing={2}>
              <FormControl fullWidth size="small" disabled={disabled}>
                <InputLabel id="host-backend-label">Edit one backend</InputLabel>
                <Select
                  labelId="host-backend-label" label="Edit one backend" value={selectedID}
                  onChange={event => setSelected(event.target.value)}
                >
                  {backends.map(backend => {
                    const target = backendHostTarget(backend.id);
                    const errors = hostFields.filter(field => edits.errors[hostErrorKey(target, field)]).length;
                    return (
                      <MenuItem key={backend.id} value={backend.id}>
                        {backend.name || backend.id} ({backend.id}){errors > 0 ? ` — ${errors} invalid` : ''}
                      </MenuItem>
                    );
                  })}
                </Select>
              </FormControl>
              {fields(backendHostTarget(selectedID), 'Selected backend')}
            </Stack>
          ) : <Alert severity="info">Add a backend to configure its CPU model.</Alert>}
        </Stack>
      </AccordionDetails>
    </Accordion>
  );
}
