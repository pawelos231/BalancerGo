import type { RunResult } from './types.ts';

export interface ResultDownload {
  readonly filename: string;
  readonly blob: Blob;
}

// Keep run and result IDs recognizable without creating paths in a filename.
export function resultExportFilename(result: RunResult, extension: 'json' | 'xlsx'): string {
  const safePart = (value: string) => value.replace(/[^a-zA-Z0-9_-]/g, '-').slice(0, 80) || 'unknown';
  return `load-balancer-result-${safePart(result.runId)}-${safePart(result.id)}.${extension}`;
}

// Export the archive itself, including exact decimal strings and unknown fields.
export function createResultJSONExport(result: RunResult): ResultDownload {
  return {
    filename: resultExportFilename(result, 'json'),
    blob: new Blob([JSON.stringify(result, null, 2), '\n'], { type: 'application/json;charset=utf-8' }),
  };
}

// The browser saves a local file; no result data is sent to the server.
export function downloadResultFile(download: ResultDownload): void {
  const url = URL.createObjectURL(download.blob);
  const anchor = document.createElement('a');
  try {
    anchor.href = url;
    anchor.download = download.filename;
    document.body.appendChild(anchor);
    anchor.click();
  } finally {
    anchor.remove();
    // Let the browser start reading the Blob before releasing its temporary URL.
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}
