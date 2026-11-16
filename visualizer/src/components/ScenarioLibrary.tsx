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
import { presetCategories } from '../scenarios/presets';
import { filterLibraryEntries, libraryCategories } from '../scenarios/library';
import type { ScenarioSummary } from '../scenarios/types';

// A loaded preset identifies the applied document until another document replaces it.
export interface AppliedPreset {
  id: string;
  title: string;
  summary: ScenarioSummary;
}

interface ScenarioLibraryProps {
  defaultsReady: boolean;
  loading: boolean;
  pending: boolean;
  validating: boolean;
  unappliedInput: boolean;
  applied: AppliedPreset | null;
  error: string | null;
  onLoad: (id: string) => void;
  onLoadSourceExample: (id: string) => void;
  onRetryDefaults: () => void;
}

// Validation applies the preset's source and native JSON without starting a run.
function ScenarioLibrary({
  defaultsReady,
  loading,
  pending,
  validating,
  unappliedInput,
  applied,
  error,
  onLoad,
  onLoadSourceExample,
  onRetryDefaults,
}: ScenarioLibraryProps) {
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('all');
  const [page, setPage] = useState(1);
  const canLoadSource = !loading && !validating && !pending && !unappliedInput;
  const canLoadPreset = defaultsReady && canLoadSource;
  const matchingEntries = filterLibraryEntries(category, search);
  const showingDiagnostics = category === 'diagnostics';

  // Keep the configuration editor within reach as the catalog grows.
  const pageSize = 6;
  const pageCount = Math.ceil(matchingEntries.length / pageSize);
  const visibleEntries = matchingEntries.slice((page - 1) * pageSize, page * pageSize);

  return (
    <Card variant="outlined">
      <CardHeader
        title="Scenario library"
        subheader="Load a runnable preset or inspect a source example in the Language editor below."
      />
      <CardContent>
        <Stack spacing={2}>
          <Typography variant="body2" color="text.secondary">
            Explore links, queues, loss, workloads, routing, reference experiments and edge cases, or run the
            unpaced simulator throughput benchmark.
            Loading a preset updates its language source, JSON and planned topology after validation.
            Distribution definitions load a valid editable source draft.
            Expected errors and unsupported runtime drafts are available through their separate category.
          </Typography>

          {showingDiagnostics && <Alert severity="info">
            These examples deliberately produce a diagnostic. They demonstrate invalid parameters
            or runtime bindings that are not supported yet; they cannot be applied unchanged.
          </Alert>}

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
              {libraryCategories.map((entry) => (
                <MenuItem key={entry.id} value={entry.id}>
                  {entry.label} ({entry.count})
                </MenuItem>
              ))}
            </TextField>
          </Stack>
          <Typography variant="body2" color="text.secondary" aria-live="polite">
            {matchingEntries.length} matching entries · {visibleEntries.length} shown
          </Typography>

          {matchingEntries.length === 0 && (
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
            {visibleEntries.map((entry) => {
              const isSourceExample = entry.kind === 'sourceExample';
              const isApplied = !isSourceExample && applied?.id === entry.id;
              let entryLabel: string | undefined;
              if (entry.kind === 'sourceExample') {
                entryLabel = entry.expectedDiagnostic ? 'Expected diagnostic' : 'Unbound definitions';
              } else {
                entryLabel = presetCategories.find(category => category.id === entry.category)?.label;
              }
              return (
                <Grid key={entry.id} size={{ xs: 12, md: 6 }} sx={{ display: 'flex' }}>
                  <Card
                    variant="outlined"
                    sx={{
                      width: '100%',
                      borderColor: isApplied ? 'primary.main' : 'divider',
                    }}
                  >
                    <CardContent>
                      <Stack spacing={1} alignItems="flex-start">
                        <Chip
                          label={entryLabel}
                          size="small"
                          variant="outlined"
                        />
                        <Typography variant="subtitle1">{entry.title}</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {entry.description}
                        </Typography>
                        {entry.kind === 'sourceExample' && entry.expectedDiagnostic && (
                          <Typography variant="caption" color="text.secondary">
                            Expected diagnostic: {entry.expectedDiagnostic}
                          </Typography>
                        )}
                        <Button
                          variant={isApplied ? 'contained' : 'outlined'}
                          disabled={isSourceExample ? !canLoadSource : !canLoadPreset}
                          aria-label={`${isSourceExample ? 'Load source example' : 'Load preset'}: ${entry.title}`}
                          onClick={() => isSourceExample ? onLoadSourceExample(entry.id) : onLoad(entry.id)}
                        >
                          {isSourceExample ? 'Load source example' : 'Load preset'}
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

          {applied && (
            <Stack spacing={1}>
              <Typography variant="subtitle2">Applied preset: {applied.title}</Typography>
              {unappliedInput ? (
                <Typography variant="body2" color="text.secondary">
                  Apply or discard the configuration edits before using this draft.
                </Typography>
              ) : (
                <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                  <Chip label="Applied and validated" color="success" size="small" />
                  <Chip label={`${applied.summary.totalClients} clients`} size="small" />
                  <Chip label={`${applied.summary.backendCount} backends`} size="small" />
                  <Chip label={`${applied.summary.clientGroupCount} groups`} size="small" />
                </Stack>
              )}
            </Stack>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

export default ScenarioLibrary;
