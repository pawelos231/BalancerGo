import {
  FormControl,
  FormHelperText,
  InputAdornment,
  InputLabel,
  MenuItem,
  Select,
  TextField,
} from '@mui/material';
import type { ScenarioField } from '../scenarios/draft';
import { algorithmHelp, scenarioNumericFields } from '../scenarios/formFields';
import HelpTooltip from './HelpTooltip';

interface ScenarioFieldInputProps {
  name: ScenarioField;
  value: string;
  error?: string;
  disabled: boolean;
  onChange: (name: ScenarioField, raw: string) => void;
  onFocus: (name: ScenarioField) => void;
  onBlur: (name: ScenarioField) => void;
}

// Each section uses the same input behavior and existing draft callbacks.
function ScenarioFieldInput({
  name,
  value,
  error,
  disabled,
  onChange,
  onFocus,
  onBlur,
}: ScenarioFieldInputProps) {
  if (name === 'algorithm') {
    return (
      <FormControl fullWidth size="small" disabled={disabled} error={Boolean(error)}>
        <InputLabel id="start-alg-label">Initial algorithm</InputLabel>

        <Select
          labelId="start-alg-label"
          label="Initial algorithm"
          value={value}
          onChange={(event) => onChange(name, event.target.value)}
          onFocus={() => onFocus(name)}
          onBlur={() => onBlur(name)}
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

        {error && <FormHelperText>{error}</FormHelperText>}
      </FormControl>
    );
  }

  const field = scenarioNumericFields[name];
  const mixed = name === 'clientGapMs' && value === '' && !error;
  const hint = mixed ? 'Mixed group intervals. Editing applies to every group.' : field.hint;

  return (
    <TextField
      fullWidth
      size="small"
      type="number"
      label={field.label}
      value={value}
      placeholder={mixed ? 'Mixed' : undefined}
      disabled={disabled}
      onChange={(event) => onChange(name, event.target.value)}
      onFocus={() => onFocus(name)}
      onBlur={() => onBlur(name)}
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
  );
}

export default ScenarioFieldInput;
