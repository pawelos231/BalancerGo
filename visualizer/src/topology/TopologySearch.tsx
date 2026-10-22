import {
  Autocomplete,
  Box,
  createFilterOptions,
  TextField,
  Typography,
} from '@mui/material';
import { useEffect, useRef, useState } from 'react';
import type { TopologySearchOption, TopologySelection } from './types';

interface TopologySearchProps {
  options: TopologySearchOption[];
  value: TopologySearchOption | null;
  onSelect: (selection: TopologySelection) => void;
}

const filterSearchOptions = createFilterOptions<TopologySearchOption>({
  limit: 50,
  stringify: (option) => `${option.label} ${option.detail} ${option.id}`,
});

// Search covers the complete physical graph, including clients hidden in groups.
function TopologySearch({ options, value, onSelect }: TopologySearchProps) {
  const [query, setQuery] = useState(value?.label ?? '');
  const typing = useRef(false);
  const selectedKey = value ? JSON.stringify([value.kind, value.id]) : null;
  const previousKey = useRef(selectedKey);

  // A growing bundle may change its label while the user is typing another query.
  useEffect(() => {
    const changedSelection = previousKey.current !== selectedKey;
    previousKey.current = selectedKey;
    if (changedSelection || !typing.current) {
      typing.current = false;
      setQuery(value?.label ?? '');
    }
  }, [selectedKey, value?.label]);

  return (
    <Autocomplete
      options={options}
      value={value}
      inputValue={query}
      onInputChange={(_, next, reason) => {
        // Selection changes are handled above; automatic resets must not erase typing.
        if (reason === 'reset') return;
        typing.current = reason === 'input';
        setQuery(next);
      }}
      onChange={(_, option) => {
        typing.current = false;
        setQuery(option?.label ?? '');
        const next = option ? { kind: option.kind, id: option.id } : null;
        onSelect(next);
      }}
      onBlur={() => {
        typing.current = false;
        setQuery(value?.label ?? '');
      }}
      getOptionLabel={(option) => option.label}
      getOptionKey={(option) => JSON.stringify([option.kind, option.id])}
      isOptionEqualToValue={(option, selected) => (
        option.id === selected.id && option.kind === selected.kind
      )}
      groupBy={(option) => option.kind === 'node' ? 'Nodes' : 'Directed links'}
      filterOptions={filterSearchOptions}
      renderOption={(props, option) => {
        const { key, ...optionProps } = props;

        return (
          <Box component="li" key={key} {...optionProps}>
            <Box>
              <Typography variant="body2">{option.label}</Typography>
              <Typography variant="caption" color="text.secondary">
                {option.detail}
              </Typography>
            </Box>
          </Box>
        );
      }}
      renderInput={(params) => (
        <TextField
          {...params}
          label="Find a client, group or directed link"
          size="small"
          helperText="Search all labels, addresses and IDs. Selecting a hidden client shows its own links. Up to 50 matches are listed."
        />
      )}
    />
  );
}

export default TopologySearch;
