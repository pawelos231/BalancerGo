import { useState } from 'react';
import { Alert, Button, MenuItem, Stack, TextField, Typography } from '@mui/material';
import { formatDuration, scenarioStateTimes } from '../scenarios/draft';
import type { TimelineEdit } from '../scenarios/timelineEdits';
import type { Scenario } from '../scenarios/types';

interface TimelineEditorProps {
  scenario: Scenario;
  selectedAt: string;
  disabled: boolean;
  blockedReason?: string;
  onSelect: (at: string) => void;
  onEdit: (edit: TimelineEdit) => string | undefined;
}

export default function TimelineEditor({
  scenario, selectedAt, disabled, blockedReason, onSelect, onEdit,
}: TimelineEditorProps) {
  const [seconds, setSeconds] = useState('');
  const [error, setError] = useState<string | null>(null);
  const locked = disabled || Boolean(blockedReason);

  function edit(type: TimelineEdit['type']) {
    const failure = onEdit(type === 'delete' ? { type } : { type, seconds });
    setError(failure ?? null);
    if (!failure) setSeconds('');
  }

  return (
    <Stack spacing={1.5}>
      <Typography variant="subtitle1">Configuration timeline</Typography>
      <TextField
        select label="Selected state" value={selectedAt} disabled={locked}
        onChange={event => { setError(null); onSelect(event.target.value); }}
        helperText="Each state is a complete configuration. The state at 0 seconds is required."
      >
        {scenarioStateTimes(scenario).map(at => (
          <MenuItem key={at} value={at}>{formatDuration(at, 9)} s ({at} ns)</MenuItem>
        ))}
      </TextField>
      <TextField
        label="New state time (virtual seconds)" type="text" value={seconds} disabled={locked}
        inputProps={{ inputMode: 'decimal' }}
        onChange={event => { setSeconds(event.target.value); setError(null); }}
        onKeyDown={event => { if (event.key === 'Enter') event.preventDefault(); }}
        helperText={`Up to 9 decimal places; must be below ${formatDuration(scenario.fixed.execution.durationNanoseconds, 9)} seconds.`}
      />
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button type="button" variant="outlined" disabled={locked} onClick={() => edit('add')}>Add state</Button>
        <Button type="button" variant="outlined" disabled={locked} onClick={() => edit('copy')}>Copy state</Button>
        <Button type="button" variant="outlined" disabled={locked || selectedAt === '0'} onClick={() => edit('move')}>Move state</Button>
        <Button type="button" color="error" disabled={locked || selectedAt === '0'} onClick={() => edit('delete')}>Delete state</Button>
      </Stack>
      <Typography variant="body2" color="text.secondary">
        Add copies the configuration active at the new time. Copy duplicates the selected state.
        Move changes its time. Delete selects the preceding state. Group counts are lifetime birth budgets;
        copying a group keeps its identity and does not restart arrivals.
      </Typography>
      {blockedReason && <Alert severity="warning">{blockedReason}</Alert>}
      {error && <Alert severity="error" sx={{ overflowWrap: 'anywhere' }}>{error}</Alert>}
    </Stack>
  );
}
