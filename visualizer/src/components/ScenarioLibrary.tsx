import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Grid,
  MenuItem,
  Pagination,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { useState } from 'react';
import { presetCategories, scenarioPresets } from '../scenarios/presets';
import type { Scenario, ScenarioSummary } from '../scenarios/types';

// A chosen candidate stays separate from the scenario currently shown in the form.
export interface PresetCandidate {
  id: string;
  title: string;
  scenario: Scenario;
}

// Manual edits keep the loaded preset's name but invalidate its validation badge.
export interface AppliedPreset {
  id: string;
  title: string;
  summary: ScenarioSummary;
  pristine: boolean;
}

interface ScenarioLibraryProps {
  defaultsReady: boolean;
  loading: boolean;
  pending: boolean;
  validating: boolean;
  candidate: PresetCandidate | null;
  applied: AppliedPreset | null;
  error: string | null;
  onChoose: (id: string) => void;
  onValidate: () => void;
  onRetryDefaults: () => void;
}

// Validation loads a preset into the form; the form owns the single Start action.
function ScenarioLibrary({
  defaultsReady,
  loading,
  pending,
  validating,
  candidate,
  applied,
  error,
  onChoose,
  onValidate,
  onRetryDefaults,
}: ScenarioLibraryProps) {
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('all');
  const [page, setPage] = useState(1);
  const canChoose = defaultsReady && !validating && !pending;
  const canValidate = candidate !== null && !validating && !pending;
  const query = search.trim().toLowerCase();
  const categories = presetCategories.map((entry) => ({
    ...entry,
    count: scenarioPresets.filter((preset) => preset.category === entry.id).length,
  }));

  const matchingPresets = scenarioPresets.filter((preset) => {
    const matchesCategory = category === 'all' || preset.category === category;
    const text = `${preset.title} ${preset.description} ${preset.id}`.toLowerCase();
    return matchesCategory && text.includes(query);
  });

  // Bound the list so a growing catalog keeps the experiment form within reach.
  const pageSize = 6;
  const pageCount = Math.ceil(matchingPresets.length / pageSize);
  const visiblePresets = matchingPresets.slice((page - 1) * pageSize, page * pageSize);

  return (
    <Card variant="outlined">
      <CardHeader
        title="Scenario library"
        subheader="Choose a preset and validate it to load its settings into the form below."
      />
      <CardContent>
        <Stack spacing={2}>
          <Typography variant="body2" color="text.secondary">
            Explore links, queues, loss, workloads, routing, reference experiments and edge cases, or run the
            unpaced simulator throughput benchmark.
            Review its settings in the form before starting. Choosing a preset does not
            change the form or start a run.
          </Typography>

          {loading && (
            <Typography variant="body2">Loading backend scenario defaults…</Typography>
          )}
          {error && <Alert severity="error">{error}</Alert>}
          {!defaultsReady && !loading && (
            <Button variant="outlined" onClick={onRetryDefaults} sx={{ alignSelf: 'flex-start' }}>
              Retry loading defaults
            </Button>
          )}

          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            <TextField
              label="Search scenarios"
              placeholder="ACK, latency, queues…"
              size="small"
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setPage(1);
              }}
              sx={{ flex: 1 }}
            />
            <TextField
              select
              label="Scenario category"
              size="small"
              value={category}
              onChange={(event) => {
                setCategory(event.target.value);
                setPage(1);
              }}
              sx={{ minWidth: { sm: 230 } }}
            >
              <MenuItem value="all">All scenarios ({scenarioPresets.length})</MenuItem>
              {categories.map((entry) => (
                <MenuItem key={entry.id} value={entry.id}>
                  {entry.label} ({entry.count})
                </MenuItem>
              ))}
            </TextField>
          </Stack>
          <Typography variant="body2" color="text.secondary" aria-live="polite">
            {matchingPresets.length} matching scenarios · {visiblePresets.length} shown
          </Typography>

          {matchingPresets.length === 0 && (
            <Alert severity="info">
              No scenarios match these filters.
              <Button
                size="small"
                onClick={() => {
                  setSearch('');
                  setCategory('all');
                  setPage(1);
                }}
              >
                Clear filters
              </Button>
            </Alert>
          )}

          <Grid container spacing={2}>
            {visiblePresets.map((preset) => {
              const selected = candidate?.id === preset.id;
              return (
                <Grid key={preset.id} size={{ xs: 12, md: 6 }} sx={{ display: 'flex' }}>
                  <Card
                    variant="outlined"
                    sx={{
                      width: '100%',
                      borderColor: selected ? 'primary.main' : 'divider',
                    }}
                  >
                    <CardContent>
                      <Stack spacing={1} alignItems="flex-start">
                        <Chip
                          label={presetCategories.find((entry) => entry.id === preset.category)?.label}
                          size="small"
                          variant="outlined"
                        />
                        <Typography variant="subtitle1">{preset.title}</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {preset.description}
                        </Typography>
                        <Button
                          variant={selected ? 'contained' : 'outlined'}
                          disabled={!canChoose || selected}
                          aria-label={`Choose ${preset.title}`}
                          onClick={() => onChoose(preset.id)}
                        >
                          {selected ? 'Chosen' : 'Choose'}
                        </Button>
                      </Stack>
                    </CardContent>
                  </Card>
                </Grid>
              );
            })}
          </Grid>

          {pageCount > 1 && (
            <Pagination
              count={pageCount}
              page={page}
              onChange={(_event, nextPage) => setPage(nextPage)}
              color="primary"
              size="small"
            />
          )}

          {candidate && (
            <Stack spacing={1} alignItems="flex-start">
              <Typography variant="subtitle2">Selected candidate: {candidate.title}</Typography>
              <Button variant="outlined" disabled={!canValidate} onClick={onValidate}>
                {validating ? 'Validating…' : 'Validate and load preset'}
              </Button>
            </Stack>
          )}

          {applied && (
            <Stack spacing={1}>
              <Typography variant="subtitle2">Loaded into form: {applied.title}</Typography>
              {applied.pristine ? (
                <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                  <Chip label="Applied and validated" color="success" size="small" />
                  <Chip label={`${applied.summary.totalClients} clients`} size="small" />
                  <Chip label={`${applied.summary.backendCount} backends`} size="small" />
                  <Chip label={`${applied.summary.clientGroupCount} groups`} size="small" />
                </Stack>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  Draft edited after loading. Start simulation validates the current settings again.
                </Typography>
              )}
            </Stack>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

export default ScenarioLibrary;
