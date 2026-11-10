import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert, Box, Button, Card, CardContent, Chip, Stack, Table, TableBody,
  TableCell, TableContainer, TableHead, TablePagination, TableRow, Typography,
} from '@mui/material';
import Inventory2OutlinedIcon from '@mui/icons-material/Inventory2Outlined';
import type { ResultsHistory, ResultsHistoryState } from '../results/history';
import type { ResultIndexEntry } from '../results/repository';
import type { RunResult } from '../results/types';
import { exactValue, resultDate, resultListEntry, virtualRange } from '../results/presentation';
import ResultDetails from '../results/ResultDetails';

interface ResultsPageProps {
  state: ResultsHistoryState;
  history: ResultsHistory;
}

interface HistoryListProps {
  entries: readonly ResultIndexEntry[];
  selectedId: string | null;
  unsaved?: boolean;
  saveErrors?: Readonly<Record<string, string>>;
  onOpen: (id: string) => void;
  onRemove: (id: string) => void;
  onRetry?: (id: string) => void;
  onRecover?: (id: string) => void;
}

function HistoryList({
  entries, selectedId, unsaved = false, saveErrors = {},
  onOpen, onRemove, onRetry, onRecover,
}: HistoryListProps) {
  const [page, setPage] = useState(0);
  const currentPage = Math.min(page, Math.max(0, Math.ceil(entries.length / 10) - 1));

  return (
    <>
      <TableContainer>
        <Table
          size="small"
          sx={{ minWidth: 780 }}
          aria-label={unsaved ? 'Unsaved results' : 'Saved results'}
        >
          <TableHead>
            <TableRow>
              <TableCell>Run / scenario context</TableCell>
              <TableCell>Completed</TableCell>
              <TableCell>Observed virtual time</TableCell>
              <TableCell>Coverage / samples</TableCell>
              <TableCell align="right">Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {entries.slice(currentPage * 10, currentPage * 10 + 10).map(entry => (
              <TableRow key={entry.id} selected={entry.id === selectedId}>
                {entry.status === 'ready' ? (
                  <>
                    <TableCell sx={{ maxWidth: 340, overflowWrap: 'anywhere' }}>
                      <Typography variant="subtitle2">Run {entry.runId}</Typography>
                      <Typography variant="body2" color="text.secondary">
                        {entry.algorithm ?? 'Algorithm unavailable'} · Seed {exactValue(entry.seed)}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">Local ID: {entry.id}</Typography>
                      {saveErrors[entry.id] && (
                        <Typography variant="body2" color="error.main" sx={{ mt: 0.5 }}>
                          {saveErrors[entry.id]}
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2">{resultDate(entry.receivedAt)}</Typography>
                      <Chip
                        size="small"
                        label={entry.completionReason === 'finished' ? 'Finished' : 'Stopped'}
                        sx={{ mt: 0.5 }}
                      />
                    </TableCell>
                    <TableCell>{virtualRange(entry.virtualTimeStartSeconds, entry.virtualTimeEndSeconds)}</TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        color={entry.complete ? 'success' : 'warning'}
                        label={entry.complete ? 'Complete' : 'Partial'}
                      />
                      <Typography variant="body2" sx={{ mt: 0.5 }}>
                        {entry.sampleCount} {entry.sampleCount === 1 ? 'sample' : 'samples'}
                      </Typography>
                    </TableCell>
                  </>
                ) : (
                  <TableCell colSpan={4} sx={{ overflowWrap: 'anywhere' }}>
                    <Typography variant="subtitle2">Local result {entry.id}</Typography>
                    <Typography variant="body2" color="warning.main">{entry.warning}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      This result needs recovery. Open previews the stored data.
                      Recover restores a valid result to history.
                    </Typography>
                  </TableCell>
                )}
                <TableCell align="right">
                  <Stack
                    direction="row" spacing={0.5} justifyContent="flex-end"
                    flexWrap="wrap" useFlexGap
                  >
                    <Button
                      size="small" onClick={() => onOpen(entry.id)}
                      aria-label={`Open result ${entry.id}`}
                    >
                      Open
                    </Button>
                    {unsaved && onRetry && (
                      <Button size="small" onClick={() => onRetry(entry.id)}>Retry save</Button>
                    )}
                    {entry.status === 'recoverable' && onRecover && (
                      <Button size="small" onClick={() => onRecover(entry.id)}>Recover</Button>
                    )}
                    <Button
                      size="small" color="error" onClick={() => onRemove(entry.id)}
                      aria-label={`${unsaved ? 'Discard' : 'Delete'} result ${entry.id}`}
                    >
                      {unsaved ? 'Discard' : 'Delete'}
                    </Button>
                  </Stack>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <TablePagination
        component="div" count={entries.length} page={currentPage} rowsPerPage={10}
        rowsPerPageOptions={[]} onPageChange={(_, next) => setPage(next)}
      />
    </>
  );
}

function EmptyHistory() {
  return (
    <Card variant="outlined">
      <CardContent sx={{ textAlign: 'center', py: 6 }}>
        <Inventory2OutlinedIcon sx={{ fontSize: 56, color: 'text.secondary', mb: 1 }} aria-hidden />
        <Typography variant="h6" component="h2">No results yet</Typography>
        <Typography color="text.secondary" sx={{ mt: 1 }}>
          Results are saved automatically when a simulation finishes successfully.
        </Typography>
        <Typography color="text.secondary" sx={{ mt: 0.5 }}>
          To save an interrupted run, choose “Stop and save to Results” in Simulation.
        </Typography>
      </CardContent>
    </Card>
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'The result action could not be completed.';
}

export default function ResultsPage({ state, history }: ResultsPageProps) {
  const [selection, setSelection] = useState<RunResult | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const detailsRef = useRef<HTMLDivElement>(null);
  const candidates = useMemo(() => state.candidates.map(resultListEntry), [state.candidates]);
  const savedEntries = useMemo(() => {
    const unsavedIds = new Set(state.candidates.map(candidate => candidate.id));
    return state.entries.filter(entry => !unsavedIds.has(entry.id));
  }, [state.entries, state.candidates]);
  const selectedCandidate = state.candidates.find(candidate => candidate.id === selection?.id);
  const selectionExists = selection !== null && (selectedCandidate !== undefined
    || state.entries.some(entry => entry.id === selection.id));
  const selected = selectionExists ? selectedCandidate ?? selection : null;
  const selectedId = selected?.id ?? null;

  // Gate the rendered detail immediately, then forget records removed by another tab.
  useEffect(() => {
    if (selection && !selectionExists) setSelection(null);
  }, [selection, selectionExists]);

  useEffect(() => {
    if (selectedId) detailsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, [selectedId]);

  function perform(action: () => void) {
    setActionError(null);
    try {
      action();
    } catch (error) {
      setActionError(errorMessage(error));
    }
  }

  function open(id: string) {
    perform(() => {
      const result = state.candidates.find(candidate => candidate.id === id) ?? history.read(id);
      if (!result) {
        setSelection(null);
        setActionError('This result is no longer available. Refresh the history to update its list.');
        history.refresh();
        return;
      }
      setSelection(result);
    });
  }

  function remove(id: string) {
    perform(() => {
      if (!history.remove(id)) {
        setActionError('The result could not be deleted. See the storage error and retry.');
        return;
      }
      if (selection?.id === id) setSelection(null);
    });
  }

  function recover(id: string) {
    perform(() => {
      const result = history.recover(id);
      if (result) setSelection(result);
      else setActionError('The result could not be recovered. See the storage error for details.');
    });
  }

  function discard(id: string) {
    perform(() => {
      history.discardCandidate(id);
      if (selection?.id === id) setSelection(null);
    });
  }

  return (
    <Stack spacing={2} sx={{ mt: 3, pb: 2 }}>
      <Stack direction="row" justifyContent="space-between" alignItems="flex-start" spacing={2}>
        <Box>
          <Typography variant="h4" component="h1">Results</Typography>
          <Typography color="text.secondary" sx={{ mt: 0.5 }}>
            History is stored in this browser and remains available without a backend connection.
          </Typography>
        </Box>
        <Button variant="outlined" onClick={() => perform(history.refresh)}>Refresh history</Button>
      </Stack>

      {actionError && <Alert severity="error" onClose={() => setActionError(null)}>{actionError}</Alert>}
      {state.storageWarnings.length > 0 && (
        <Alert severity="warning">
          {state.storageWarnings.map((warning, index) => <Box key={index}>{warning}</Box>)}
        </Alert>
      )}
      {state.preparing.length > 0 && (
        <Alert severity="info">
          Preparing {state.preparing.length} {state.preparing.length === 1 ? 'result' : 'results'}
          {' '}while their run metadata is resolved.
        </Alert>
      )}

      {candidates.length > 0 && (
        <Card variant="outlined">
          <CardContent sx={{ pb: 1 }}>
            <Typography variant="h6" component="h2">Unsaved results ({candidates.length})</Typography>
            <Typography variant="body2" color="warning.main" sx={{ mt: 0.5 }}>
              These results remain in memory. Keep this tab open until saving succeeds.
              Discard permanently releases the in-memory candidate.
            </Typography>
          </CardContent>
          <HistoryList
            entries={candidates} selectedId={selectedId} unsaved saveErrors={state.saveErrors}
            onOpen={open} onRetry={id => perform(() => history.retry(id))}
            onRemove={discard}
          />
        </Card>
      )}

      {savedEntries.length > 0 ? (
        <Card variant="outlined">
          <CardContent sx={{ pb: 1 }}>
            <Typography variant="h6" component="h2">Saved history ({savedEntries.length})</Typography>
          </CardContent>
          <HistoryList
            entries={savedEntries} selectedId={selectedId}
            onOpen={open} onRemove={remove} onRecover={recover}
          />
        </Card>
      ) : candidates.length === 0 && state.preparing.length === 0 && <EmptyHistory />}

      {selected && (
        <Box ref={detailsRef} sx={{ scrollMarginTop: 16 }}>
          <Stack
            direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between"
            alignItems={{ xs: 'flex-start', sm: 'center' }} spacing={1} sx={{ mb: 1 }}
          >
            <Typography variant="overline" color="text.secondary">
              {selectedCandidate ? 'In-memory result' : 'Saved result'}
            </Typography>
            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
              {selectedCandidate && (
                <Button size="small" onClick={() => perform(() => history.retry(selected.id))}>Retry save</Button>
              )}
              <Button
                size="small" color="error"
                onClick={() => selectedCandidate ? discard(selected.id) : remove(selected.id)}
              >
                {selectedCandidate ? 'Discard' : 'Delete'}
              </Button>
              <Button size="small" onClick={() => setSelection(null)}>Close details</Button>
            </Stack>
          </Stack>
          <ResultDetails key={selected.id} result={selected} />
        </Box>
      )}
    </Stack>
  );
}
