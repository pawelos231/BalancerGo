import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Stack,
  Typography,
  ToggleButton,
  ToggleButtonGroup,
} from '@mui/material';
import { useRef } from 'react';
import type { ReactNode } from 'react';
import { Link as RouterLink } from 'react-router';
import type { Scenario, ValidatedScenario } from '../scenarios/types';
import ScenarioDocumentPreview from './ScenarioDocumentPreview';
import ScenarioJsonEditor from './ScenarioJsonEditor';

export interface ScenarioFileNotice {
  severity: 'success' | 'error';
  message: string;
}

export type ScenarioEditorMode = 'json' | 'language';

interface ScenarioFilesProps {
  scenario: Scenario | null;
  validation: ValidatedScenario | null;
  busy: boolean;
  checking: boolean;
  notice: ScenarioFileNotice | null;
  document: string;
  unapplied: boolean;
  jsonDirty: boolean;
  jsonBlocked: boolean;
  pendingEdits: boolean;
  editorMode: ScenarioEditorMode;
  onEditorModeChange: (mode: ScenarioEditorMode) => void;
  languageEditor: (configuration: ReactNode) => ReactNode;
  onEditDocument: (document: string) => void;
  onApplyDocument: () => void;
  onDiscardDocument: () => void;
  onImport: (file: File) => void;
  onExport: () => void;
  onValidate: () => void;
  canStart: boolean;
  onStart: () => void;
}

// Files and presets use the same draft; importing only prepares the next Start.
function ScenarioFiles({
  scenario,
  validation,
  busy,
  checking,
  notice,
  document: scenarioDocument,
  unapplied,
  jsonDirty,
  jsonBlocked,
  pendingEdits,
  editorMode,
  onEditorModeChange,
  languageEditor,
  onEditDocument,
  onApplyDocument,
  onDiscardDocument,
  onImport,
  onExport,
  onValidate,
  canStart,
  onStart,
}: ScenarioFilesProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const canUseDraft = scenario !== null && !busy && !unapplied;

  const configurationControls = (
    <Stack spacing={2} sx={{ minWidth: 0 }}>
      <input
        ref={inputRef}
        type="file"
        accept=".json,application/json"
        aria-label="Scenario JSON file"
        disabled={busy || pendingEdits}
        hidden
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          // Allow choosing the same file again after an error or an edit.
          event.currentTarget.value = '';
          if (file) onImport(file);
        }}
      />
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button
          variant="outlined" disabled={busy || pendingEdits}
          onClick={() => inputRef.current?.click()}
        >
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
      {validation && !unapplied && (
        <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
          <Chip label="Draft validated" color="success" size="small" />
          <Chip label={`${validation.summary.totalClients} clients`} size="small" />
          <Chip label={`${validation.summary.backendCount} backends`} size="small" />
          <Chip label={`${validation.summary.clientGroupCount} groups`} size="small" />
        </Stack>
      )}

      {validation?.resolved && !unapplied ? (
        <ScenarioDocumentPreview
          title="Effective configuration"
          document={validation.resolved}
          description={
            'Read-only backend result after resolving inherited settings, profiles and overrides. ' +
            'Groups remain compact. Export uses the applied native scenario.'
          }
        />
      ) : (
        <Typography variant="caption" color="text.secondary">
          Validate the draft to inspect its effective configuration.
        </Typography>
      )}
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        <Button variant="contained" disabled={!canStart} onClick={onStart}>
          Start simulation
        </Button>
        <Button component={RouterLink} to="/planned-topology" variant="outlined">
          Preview planned topology
        </Button>
      </Stack>
      <Typography variant="caption" color="text.secondary">
        Start uses the applied JSON document. Planned topology shows its resources,
        links and declared changes over time.
      </Typography>
    </Stack>
  );

  return (
    <Card variant="outlined">
      <CardHeader title="Scenario configuration" />
      <CardContent sx={{ pt: 0 }}>
        <Stack spacing={2} sx={{ minWidth: 0 }}>
          <Typography variant="body2" color="text.secondary">
            JSON and Language prepare one applied scenario for Start and planned topology.
            Apply or discard pending edits before changing format. Loading a preset replaces
            both source and JSON. Importing JSON preserves your source text.
          </Typography>
          <ToggleButtonGroup
            value={editorMode} exclusive size="small" aria-label="Configuration format"
            disabled={busy || pendingEdits}
            onChange={(_, value: ScenarioEditorMode | null) => { if (value) onEditorModeChange(value); }}
            sx={{ alignSelf: 'flex-start' }}
          >
            <ToggleButton value="json">JSON</ToggleButton>
            <ToggleButton value="language">Language</ToggleButton>
          </ToggleButtonGroup>

          <Box sx={{ display: editorMode === 'json' ? 'block' : 'none' }}>
            {scenario && (
              <ScenarioJsonEditor
                document={scenarioDocument}
                dirty={jsonDirty}
                disabled={busy || jsonBlocked || editorMode !== 'json'}
                onChange={onEditDocument}
                onApply={onApplyDocument}
                onDiscard={onDiscardDocument}
              />
            )}
            {editorMode === 'json' && <Box sx={{ mt: 2 }}>{configurationControls}</Box>}
          </Box>
          <Box sx={{ display: editorMode === 'language' ? 'block' : 'none', minWidth: 0 }}>
            {languageEditor(editorMode === 'language' ? configurationControls : null)}
          </Box>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default ScenarioFiles;
