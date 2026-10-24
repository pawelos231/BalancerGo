import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Stack,
  Typography,
} from '@mui/material';
import { useRef } from 'react';
import type { Scenario, ValidatedScenario } from '../scenarios/types';
import ScenarioDocumentPreview from './ScenarioDocumentPreview';

export interface ScenarioFileNotice {
  severity: 'success' | 'error';
  message: string;
}

interface ScenarioFilesProps {
  scenario: Scenario | null;
  validation: ValidatedScenario | null;
  busy: boolean;
  checking: boolean;
  invalid: boolean;
  notice: ScenarioFileNotice | null;
  onImport: (file: File) => void;
  onExport: () => void;
  onValidate: () => void;
}

// Files and presets use the same draft; importing only prepares the next Start.
function ScenarioFiles({
  scenario,
  validation,
  busy,
  checking,
  invalid,
  notice,
  onImport,
  onExport,
  onValidate,
}: ScenarioFilesProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const canUseDraft = scenario !== null && !busy && !invalid;

  return (
    <Card variant="outlined">
      <CardHeader title="Scenario files" />
      <CardContent sx={{ pt: 0 }}>
        <Stack spacing={2} sx={{ minWidth: 0 }}>
          <Typography variant="body2" color="text.secondary">
            Import a scenario JSON file to validate and load it into the form. Export saves
            the full draft for the next Start, including profiles and client overrides.
            Files can be up to 2 MiB. Import does not start a simulation.
          </Typography>

          <input
            ref={inputRef}
            type="file"
            accept=".json,application/json"
            aria-label="Scenario JSON file"
            disabled={busy}
            hidden
            onChange={(event) => {
              const file = event.currentTarget.files?.[0];
              // Allow choosing the same file again after an error or an edit.
              event.currentTarget.value = '';
              if (file) onImport(file);
            }}
          />
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            <Button variant="outlined" disabled={busy} onClick={() => inputRef.current?.click()}>
              Import JSON
            </Button>
            <Button variant="outlined" disabled={!canUseDraft} onClick={onExport}>
              Export JSON
            </Button>
            <Button variant="outlined" disabled={!canUseDraft} onClick={onValidate}>
              Validate draft
            </Button>
          </Stack>

          {checking && <Typography variant="body2">Checking scenario…</Typography>}
          {notice && (
            <Alert severity={notice.severity} sx={{ overflowWrap: 'anywhere' }}>
              {notice.message}
            </Alert>
          )}
          {invalid && (
            <Alert severity="warning">
              Fix the highlighted fields before validation or export. The preview keeps
              their last valid values.
            </Alert>
          )}
          {validation && (
            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
              <Chip label="Draft validated" color="success" size="small" />
              <Chip label={`${validation.summary.totalClients} clients`} size="small" />
              <Chip label={`${validation.summary.backendCount} backends`} size="small" />
              <Chip label={`${validation.summary.clientGroupCount} groups`} size="small" />
            </Stack>
          )}

          {scenario && (
            <ScenarioDocumentPreview
              title="Native scenario"
              document={scenario}
              description={
                'The full draft used for export and Start. Profiles and overrides are preserved; ' +
                'durations are in nanoseconds.'
              }
            />
          )}
          {validation?.resolved ? (
            <ScenarioDocumentPreview
              title="Effective configuration"
              document={validation.resolved}
              description={
                'Read-only backend result after resolving inherited settings, profiles and overrides. ' +
                'Groups remain compact. Export uses Native scenario.'
              }
            />
          ) : (
            <Typography variant="caption" color="text.secondary">
              Validate the draft to inspect its effective configuration.
            </Typography>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

export default ScenarioFiles;
