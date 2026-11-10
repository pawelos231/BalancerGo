import { useState } from 'react';
import { Alert, Button, Stack, Typography } from '@mui/material';
import DownloadIcon from '@mui/icons-material/Download';
import type { RunResult } from './types';
import { createResultJSONExport, downloadResultFile } from './export';

export default function ResultExports({ result }: { result: RunResult }) {
  const [error, setError] = useState<string | null>(null);

  function exportJSON() {
    setError(null);
    try {
      downloadResultFile(createResultJSONExport(result));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'The browser could not create the file.';
      setError(`JSON export failed: ${message}`);
    }
  }

  return (
    <Stack spacing={1}>
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button variant="outlined" startIcon={<DownloadIcon />} onClick={exportJSON}>Export JSON</Button>
      </Stack>
      <Typography variant="body2" color="text.secondary">
        Exports the full result: configuration, raw samples, topology definitions, summary and observation coverage.
      </Typography>
      {error && <Alert severity="error">{error}</Alert>}
    </Stack>
  );
}
