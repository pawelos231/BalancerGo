import {
  Checkbox,
  FormControl,
  FormControlLabel,
  FormHelperText,
  InputAdornment,
  InputLabel,
  MenuItem,
  Select,
  TextField,
} from '@mui/material';
import type { ScenarioField } from '../scenarios/draft';
import {
  rtoBackoffHelp,
  scenarioNumericFields,
  scenarioSelectFields,
} from '../scenarios/formFields';
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
  if (name === 'rtoBackoff') {
    return (
      <FormControl disabled={disabled} error={Boolean(error)}>
        <FormControlLabel
          label="Exponential timeout backoff"
          control={(
            <Checkbox
              checked={value === 'true'}
              onChange={(event) => onChange(name, String(event.target.checked))}
              onFocus={() => onFocus(name)}
              onBlur={() => onBlur(name)}
            />
          )}
        />
        <FormHelperText>
          {error ?? rtoBackoffHelp}
        </FormHelperText>
      </FormControl>
    );
  }

  if (name === 'algorithm' || name === 'rtoMode' || name === 'congestionControl') {
    const { label, help, options } = scenarioSelectFields[name];
    const labelID = `start-${name}-label`;

    return (
      <FormControl fullWidth size="small" disabled={disabled} error={Boolean(error)}>
        <InputLabel id={labelID}>{label}</InputLabel>

        <Select
          labelId={labelID}
          label={label}
          value={value}
          onChange={(event) => onChange(name, event.target.value)}
          onFocus={() => onFocus(name)}
          onBlur={() => onBlur(name)}
          endAdornment={(
            <InputAdornment position="end" sx={{ mr: 3 }}>
              <HelpTooltip label={label}>
                {help}
              </HelpTooltip>
            </InputAdornment>
          )}
        >
          {options.map((option) => (
            <MenuItem key={option.value} value={option.value}>{option.label}</MenuItem>
          ))}
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
