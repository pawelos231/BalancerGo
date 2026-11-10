import { useState } from 'react';
import { Alert, Button, CircularProgress, Stack, Typography } from '@mui/material';
import DownloadIcon from '@mui/icons-material/Download';
import type { RunResult } from './types';
import { createResultJSONExport, downloadResultFile } from './export';

export default function ResultExports({ result }: { result: RunResult }) {
  const [error, setError] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);
  const [progressRows, setProgressRows] = useState(0);

  function exportJSON() {
    setError(null);
    try {
      downloadResultFile(createResultJSONExport(result));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'The browser could not create the file.';
      setError(`JSON export failed: ${message}`);
    }
  }

  async function exportXLSX() {
    if (generating) return;
    setError(null);
    setProgressRows(0);
    setGenerating(true);
    try {
      // Load spreadsheet support only when requested; the full JSON stays unchanged.
      const { createResultXlsxExport } = await import('./xlsx');
      const file = await createResultXlsxExport(result, { onProgress: setProgressRows });
      downloadResultFile(file);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'The browser could not create the workbook.';
      setError(`XLSX export failed: ${message}`);
    } finally {
      setGenerating(false);
    }
  }

  return (
    <Stack spacing={1}>
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button variant="outlined" startIcon={<DownloadIcon />} onClick={exportJSON} disabled={generating}>
          Export JSON
        </Button>
        <Button
          variant="outlined" disabled={generating} onClick={() => { void exportXLSX(); }}
          startIcon={generating ? <CircularProgress size={16} /> : <DownloadIcon />}
        >
          {generating ? 'Preparing XLSX…' : 'Export XLSX'}
        </Button>
      </Stack>
      <Typography variant="body2" color="text.secondary">
        Exports the full result: configuration, raw samples, topology definitions, summary and observation coverage.
      </Typography>
      {generating && (
        <Typography role="status" variant="body2" color="text.secondary">
          {progressRows === 0 ? 'Checking export size…' : `Prepared ${progressRows.toLocaleString()} workbook rows…`}
        </Typography>
      )}
      {error && <Alert severity="error">{error}</Alert>}
    </Stack>
  );
}
