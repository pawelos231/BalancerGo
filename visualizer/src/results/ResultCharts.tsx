import { useMemo, useState } from 'react';
import { Autocomplete, Box, Stack, TextField, Typography } from '@mui/material';
import type { RunResult } from './types';
import { buildResultChartSamples, chartBackends, filterChartBackends, resultChartDefinitions } from './chartData';
import ResultHistoryChart from './ResultHistoryChart';

export default function ResultCharts({ result }: { result: RunResult }) {
  const [selectedBackendId, setSelectedBackendId] = useState<string | null>(null);
  const backends = useMemo(() => chartBackends(result), [result]);
  const backend = backends.find(value => value.id === selectedBackendId) ?? backends[0] ?? null;
  const backendId = backend?.id ?? null;
  const samples = useMemo(() => buildResultChartSamples(result, backendId), [result, backendId]);
  const revisionChanges = samples.filter((sample, index) => (
    index > 0 && sample.topologyRevision !== samples[index - 1].topologyRevision
  )).length;
  const scheduledChanges = Object.keys(result.context.scenario?.states ?? {}).filter(value => value !== '0').length;

  return (
    <Stack spacing={2}>
      <Typography variant="h6" component="h3">Metric history</Typography>
      <Typography variant="body2" color="text.secondary">
        Samples follow publication order and use virtual time on the horizontal axis; equal timestamps remain distinct.
        {' '}Topology revision transitions: {revisionChanges}. Scheduled configuration changes after state 0: {scheduledChanges}.
        {' '}Sample inspection includes the matching topology revision. These are virtual-time metrics;
        engine execution rates in real time appear in the summary.
      </Typography>
      <Autocomplete
        options={backends} value={backend} disableClearable={backends.length > 0}
        getOptionLabel={option => option.label === option.id ? option.id : `${option.label} (${option.id})`}
        getOptionKey={option => option.id}
        isOptionEqualToValue={(option, value) => option.id === value.id}
        filterOptions={(options, state) => filterChartBackends(options, state.inputValue)}
        onChange={(_, value) => setSelectedBackendId(value?.id ?? null)}
        noOptionsText="No matching observed backends"
        renderInput={params => (
          <TextField {...params} label="Backend for CPU and connection p95"
            helperText={`${backends.length} observed backends; up to 20 search matches shown. Search by name or ID.`} />
        )}
        sx={{ maxWidth: 620 }}
      />
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', xl: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
        {resultChartDefinitions.map(definition => (
          <ResultHistoryChart key={`${result.id}:${backendId}:${definition.title}`} samples={samples} definition={definition} />
        ))}
      </Box>
    </Stack>
  );
}
