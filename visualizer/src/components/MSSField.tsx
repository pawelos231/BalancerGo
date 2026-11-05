import { Autocomplete, InputAdornment, TextField } from '@mui/material';

import { scenarioNumericFields } from '../scenarios/formFields';
import HelpTooltip from './HelpTooltip';

interface MSSFieldProps {
  value: string;
  error?: string;
  disabled: boolean;
  onChange: (raw: string) => void;
  onFocus: () => void;
  onBlur: () => void;
}

// These are payload limits derived from IPv4 MTUs, not new network technologies.
const suggestions = [
  { value: '1024', label: '1024 B — current simulator profile' },
  { value: '1460', label: '1460 B — IPv4, MTU 1500' },
  { value: '1452', label: '1452 B — IPv4, MTU 1492 (PPPoE)' },
  { value: '536', label: '536 B — IPv4, small MTU 576' },
  { value: '8960', label: '8960 B — IPv4, jumbo MTU 9000' },
];

// Selecting a suggestion and typing a custom number use the same MSS field.
export default function MSSField({
  value,
  error,
  disabled,
  onChange,
  onFocus,
  onBlur,
}: MSSFieldProps) {
  const field = scenarioNumericFields.mssBytes;

  return (
    <Autocomplete
      freeSolo
      fullWidth
      size="small"
      disabled={disabled}
      value={value}
      inputValue={value}
      options={suggestions.map((option) => option.value)}
      renderOption={({ key, ...optionProps }, option) => (
        <li key={key} {...optionProps}>
          {suggestions.find((entry) => entry.value === option)?.label ?? option}
        </li>
      )}
      onChange={(_, selected) => onChange(selected ?? '')}
      onInputChange={(_, raw, reason) => {
        if (reason === 'input' || reason === 'clear') onChange(raw);
      }}
      onFocus={onFocus}
      onBlur={onBlur}
      renderInput={(params) => (
        <TextField
          {...params}
          label={field.label}
          error={Boolean(error)}
          helperText={error ?? 'Choose a suggested size or enter your own payload limit.'}
          inputProps={{ ...params.inputProps, inputMode: 'numeric' }}
          slotProps={{
            input: {
              ...params.InputProps,
              endAdornment: (
                <>
                  <InputAdornment position="end">
                    <HelpTooltip label={field.label}>{field.help}</HelpTooltip>
                  </InputAdornment>
                  {params.InputProps.endAdornment}
                </>
              ),
            },
          }}
        />
      )}
    />
  );
}
