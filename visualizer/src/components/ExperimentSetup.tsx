import { Alert, Button, Card, CardContent, CardHeader, Stack } from '@mui/material';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { formatScenarioDocument, readScenarioFile, serializeScenario } from '../scenarios/files';
import { validateEditorDocument } from '../scenarios/editor';
import { scenarioPresets } from '../scenarios/presets';
import { scenarioApi } from '../scenarios/runtimeApi';
import { validateScenario } from '../scenarios/validation';
import type { CurrentSimulation, Scenario, ValidatedScenario } from '../scenarios/types';
import type { SimulationMetadata } from '../types';
import ScenarioLibrary from './ScenarioLibrary';
import type { AppliedPreset } from './ScenarioLibrary';
import ScenarioFiles from './ScenarioFiles';
import type { ScenarioFileNotice } from './ScenarioFiles';
import ScenarioRunDetails from './ScenarioRunDetails';

interface ExperimentSetupProps {
  connected: boolean;
  simulation: SimulationMetadata | null;
  pending: boolean;
  currentRun: CurrentSimulation | null;
  currentRunError: string | null;
  onRefreshCurrent: () => void;
  onStart: (scenario: Scenario) => Promise<boolean>;
  onPreviewChange: (scenario: Scenario | null, error: string | null) => void;
}

// JSON, files and presets share one applied scenario, independently of the active run.
function ExperimentSetup({
  connected,
  simulation,
  pending,
  currentRun,
  currentRunError,
  onRefreshCurrent,
  onStart,
  onPreviewChange,
}: ExperimentSetupProps) {
  const [defaults, setDefaults] = useState<Scenario | null>(null);
  const [scenario, setScenario] = useState<Scenario | null>(null);
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [applied, setApplied] = useState<AppliedPreset | null>(null);
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validation, setValidation] = useState<ValidatedScenario | null>(null);
  const [fileNotice, setFileNotice] = useState<ScenarioFileNotice | null>(null);
  const [editedDocument, setEditedDocument] = useState<string | null>(null);
  const editedDocumentRef = useRef<string | null>(null);
  const validationRef = useRef<AbortController | null>(null);
  const scenarioRef = useRef<Scenario | null>(null);
  const appliedDocument = useMemo(() => scenario ? formatScenarioDocument(scenario) : '', [scenario]);
  const unappliedDocument = editedDocument !== null;
  const canStart = scenario !== null && connected && simulation?.status !== 'running'
    && !loading && !pending && !validating && !unappliedDocument;

  const setNativeScenario = useCallback((definition: Scenario) => {
    scenarioRef.current = definition;
    setScenario(definition);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    scenarioApi.defaults(controller.signal).then((definition) => {
      if (controller.signal.aborted) return;
      setDefaults(definition);
      // A late defaults response must not overwrite an already imported draft.
      if (!scenarioRef.current) {
        setNativeScenario(definition);
      }
      setError(null);
    }).catch((failure: unknown) => {
      if (controller.signal.aborted) return;
      setError(failure instanceof Error ? failure.message : 'Cannot load scenario defaults.');
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });

    return () => controller.abort();
  }, [reload, setNativeScenario]);

  useEffect(() => () => {
    validationRef.current?.abort();
    validationRef.current = null;
  }, []);

  // Preview the applied document; unfinished JSON edits never replace it.
  useEffect(() => {
    if (!scenario) return;
    if (unappliedDocument) {
      onPreviewChange(null, 'JSON edits have not been applied. Apply or discard them to update the planned topology.');
      return;
    }
    try {
      // Fill omitted optional fields for the topology projection.
      onPreviewChange(validateScenario(scenario), null);
    } catch (failure) {
      const message = failure instanceof Error ? failure.message : 'Cannot preview the applied scenario.';
      onPreviewChange(null, message);
    }
  }, [scenario, unappliedDocument, onPreviewChange]);

  // Lock every draft action until reading and server validation have both finished.
  // Keep the old applied document unless the complete operation succeeds.
  async function validateAndApply(
    load: (signal: AbortSignal) => Promise<ValidatedScenario>,
    onApplied: (result: ValidatedScenario) => void,
  ) {
    if (validationRef.current || pending) return;
    const controller = new AbortController();
    validationRef.current = controller;
    setValidating(true);
    setError(null);
    setFileNotice(null);

    try {
      const result = await load(controller.signal);
      if (controller.signal.aborted || validationRef.current !== controller) return;
      setNativeScenario(result.scenario);
      editedDocumentRef.current = null;
      setEditedDocument(null);
      setValidation(result);
      onApplied(result);
    } catch (failure) {
      if (!controller.signal.aborted && validationRef.current === controller) {
        setFileNotice({
          severity: 'error',
          message: failure instanceof Error ? failure.message : 'Scenario validation failed.',
        });
      }
    } finally {
      if (validationRef.current === controller) {
        validationRef.current = null;
        if (!controller.signal.aborted) setValidating(false);
      }
    }
  }

  // A single click validates the preset; only success replaces the applied document.
  function loadPreset(id: string) {
    if (!defaults || loading || validationRef.current || pending || hasUnappliedDocument()) return;
    const preset = scenarioPresets.find(entry => entry.id === id);
    if (!preset) return;

    void validateAndApply(
      (signal) => scenarioApi.validate(preset.build(defaults), signal),
      (result) => {
        setApplied({
          id: preset.id,
          title: preset.title,
          summary: result.summary,
        });
        setFileNotice({ severity: 'success', message: `Loaded preset: ${preset.title}.` });
      },
    );
  }

  function importScenario(file: File) {
    if (hasUnappliedDocument()) return;
    void validateAndApply(
      async (signal) => {
        const document = await readScenarioFile(file);
        signal.throwIfAborted();
        return scenarioApi.validateDocument(document, signal);
      },
      () => {
        setApplied(null);
        setFileNotice({ severity: 'success', message: `Imported ${file.name}. Ready for Start.` });
      },
    );
  }

  function hasUnappliedDocument() {
    return editedDocumentRef.current !== null;
  }

  function changeDocument(document: string) {
    if (!scenarioRef.current || validationRef.current || pending) return;
    const next = document === appliedDocument ? null : document;
    editedDocumentRef.current = next;
    setEditedDocument(next);
    setFileNotice(null);
  }

  function discardDocument() {
    if (validationRef.current || pending) return;
    editedDocumentRef.current = null;
    setEditedDocument(null);
    setFileNotice(null);
  }

  function applyDocument() {
    const document = editedDocumentRef.current;
    if (document === null) return;
    void validateAndApply(
      signal => validateEditorDocument(document, scenarioApi, signal),
      () => {
        setApplied(null);
        setFileNotice({ severity: 'success', message: 'Applied JSON document. Ready for Start.' });
      },
    );
  }

  function validateDraft() {
    const current = scenarioRef.current;
    if (!current || hasUnappliedDocument()) return;
    void validateAndApply(
      (signal) => scenarioApi.validate(current, signal),
      () => setFileNotice({
        severity: 'success',
        message: 'Draft validated. Effective configuration is available below.',
      }),
    );
  }

  function exportScenario() {
    const current = scenarioRef.current;
    if (!current || hasUnappliedDocument()) return;
    let document = '';

    // Reuse validation while the applied document is unchanged.
    void validateAndApply(
      async (signal) => {
        const checked = validation?.scenario === current
          ? validation
          : await scenarioApi.validate(current, signal);
        document = serializeScenario(checked.scenario);
        return checked;
      },
      () => {
        // Browser side effects run only after the current-operation guard.
        downloadScenario(document);
        setFileNotice({ severity: 'success', message: 'Exported scenario.json.' });
      },
    );
  }

  function startSimulation() {
    const current = scenarioRef.current;
    if (!current || !canStart || validationRef.current || hasUnappliedDocument()) return;
    void onStart(current);
  }

  function reloadDefaults() {
    setLoading(true);
    setError(null);
    setReload((revision) => revision + 1);
  }

  return (
    <Stack spacing={2}>
      <ScenarioLibrary
        defaultsReady={defaults !== null}
        loading={loading}
        pending={pending}
        validating={validating}
        unappliedInput={unappliedDocument}
        applied={applied}
        error={error}
        onLoad={loadPreset}
        onRetryDefaults={reloadDefaults}
      />

      <ScenarioFiles
        scenario={scenario}
        validation={validation}
        busy={loading || pending || validating}
        checking={validating}
        notice={fileNotice}
        document={editedDocument ?? appliedDocument}
        unapplied={unappliedDocument}
        onEditDocument={changeDocument}
        onApplyDocument={applyDocument}
        onDiscardDocument={discardDocument}
        onImport={importScenario}
        onExport={exportScenario}
        onValidate={validateDraft}
        canStart={canStart}
        onStart={startSimulation}
      />

      {!scenario && (
        <Alert severity="info">Load the server defaults or import a scenario JSON file to begin.</Alert>
      )}

      {currentRunError && (
        <Stack spacing={1}>
          <Alert severity="error">{currentRunError}</Alert>
          <Button
            variant="outlined"
            onClick={onRefreshCurrent}
            sx={{ alignSelf: 'flex-start' }}
          >
            Retry current scenario
          </Button>
        </Stack>
      )}
      {currentRun && (
        <Card variant="outlined">
          <CardHeader title="Run details" />
          <CardContent><ScenarioRunDetails run={currentRun} /></CardContent>
        </Card>
      )}
    </Stack>
  );
}

export default ExperimentSetup;

// A temporary attached link lets the browser save the validated native document.
function downloadScenario(contents: string) {
  const blob = new Blob([contents], { type: 'application/json;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  try {
    anchor.href = url;
    anchor.download = 'scenario.json';
    document.body.appendChild(anchor);
    anchor.click();
  } finally {
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}
