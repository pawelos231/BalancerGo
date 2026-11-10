import { Alert, Button, Chip, Stack, TextField, Typography } from '@mui/material';

interface ScenarioJsonEditorProps {
  document: string;
  dirty: boolean;
  disabled: boolean;
  onChange: (document: string) => void;
  onApply: () => void;
  onDiscard: () => void;
}

export default function ScenarioJsonEditor({
  document, dirty, disabled, onChange, onApply, onDiscard,
}: ScenarioJsonEditorProps) {
  return (
    <Stack spacing={1.5} sx={{ minWidth: 0 }}>
      <Typography variant="body2" color="text.secondary">
        Edit or paste the complete scenario, including fixed settings and every state.
        Apply checks the JSON locally and on the server before replacing the applied draft.
        Durations are in nanoseconds. Keep large integers as quoted decimal strings.
      </Typography>
      <Chip
        label={dirty ? 'Unapplied JSON edits' : 'Applied draft'}
        color={dirty ? 'warning' : 'default'}
        size="small"
        sx={{ alignSelf: 'flex-start' }}
      />
      <TextField
        label="Scenario JSON document"
        value={document}
        onChange={event => onChange(event.target.value)}
        disabled={disabled}
        multiline
        fullWidth
        minRows={14}
        maxRows={28}
        slotProps={{
          htmlInput: { spellCheck: false, wrap: 'off' },
          input: { sx: { fontFamily: 'monospace', fontSize: 13 } },
        }}
        helperText="Up to 2 MiB of UTF-8 JSON. Applying a document does not start a simulation."
      />
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button variant="contained" disabled={disabled || !dirty} onClick={onApply}>
          Apply JSON
        </Button>
        <Button variant="outlined" disabled={disabled || !dirty} onClick={onDiscard}>
          Discard JSON edits
        </Button>
      </Stack>
      {dirty && (
        <Alert severity="warning">
          JSON edits have not been applied. Apply or discard them to use the form, presets,
          import, export or Start. The applied draft is retained below.
        </Alert>
      )}
    </Stack>
  );
}
