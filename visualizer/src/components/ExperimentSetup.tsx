import { Alert, Button, Card, CardContent, CardHeader, Stack } from '@mui/material';
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  initialLanguageSource, loadLanguageSource, serializeLanguageSource, validateLanguageDocument,
} from '../scenario-language/editor';
import { LanguageError } from '../scenario-language/source';
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
import type { ScenarioEditorMode, ScenarioFileNotice } from './ScenarioFiles';
import ScenarioRunDetails from './ScenarioRunDetails';

const ScenarioLanguageEditor = lazy(() => import('./ScenarioLanguageEditor'));

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
  const [languageSource, setLanguageSource] = useState(initialLanguageSource);
  const languageSourceRef = useRef(initialLanguageSource);
  const [languageBaseline, setLanguageBaseline] = useState(initialLanguageSource);
  const languageBaselineRef = useRef(initialLanguageSource);
  const [languageScenario, setLanguageScenario] = useState<Scenario | null>(null);
  const languageScenarioRef = useRef<Scenario | null>(null);
  const [languageError, setLanguageError] = useState<LanguageError | null>(null);
  const [editorMode, setEditorMode] = useState<ScenarioEditorMode>('language');
  const editorModeRef = useRef<ScenarioEditorMode>('language');
  const validationRef = useRef<AbortController | null>(null);
  const scenarioRef = useRef<Scenario | null>(null);
  const appliedDocument = useMemo(() => scenario ? formatScenarioDocument(scenario) : '', [scenario]);
  const unappliedJSON = editedDocument !== null;
  const unappliedLanguage = languageSource !== languageBaseline;
  const languageApplied = languageScenario !== null && languageScenario === scenario && !unappliedLanguage;
  const pendingEdits = unappliedJSON || unappliedLanguage;
  const sourceNeedsApply = editorMode === 'language' && !languageApplied;
  const unappliedDocument = pendingEdits || sourceNeedsApply;
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
      const message = sourceNeedsApply && !pendingEdits
        ? 'Language source does not describe the applied JSON. Compile and apply it or select JSON.'
        : 'Configuration edits have not been applied. Apply or discard them to update the planned topology.';
      onPreviewChange(null, message);
      return;
    }
    try {
      // Fill omitted optional fields for the topology projection.
      onPreviewChange(validateScenario(scenario), null);
    } catch (failure) {
      const message = failure instanceof Error ? failure.message : 'Cannot preview the applied scenario.';
      onPreviewChange(null, message);
    }
  }, [scenario, unappliedDocument, pendingEdits, sourceNeedsApply, onPreviewChange]);

  // Lock every draft action until reading and server validation have both finished.
  // Keep the old applied document unless the complete operation succeeds.
  async function validateAndApply(
    load: (signal: AbortSignal) => Promise<ValidatedScenario>,
    onApplied: (result: ValidatedScenario) => void,
    onFailed?: (failure: unknown) => void,
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
        onFailed?.(failure);
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

  // Only successful validation replaces the preset source and its generated JSON together.
  function loadPreset(id: string) {
    if (!defaults || loading || validationRef.current || pending || hasUnappliedDocument()) return;
    const preset = scenarioPresets.find(entry => entry.id === id);
    if (!preset) return;

    void validateAndApply(
      signal => validateLanguageDocument(preset.source, scenarioApi, signal),
      (result) => {
        languageSourceRef.current = preset.source;
        setLanguageSource(preset.source);
        languageBaselineRef.current = preset.source;
        setLanguageBaseline(preset.source);
        setLanguageError(null);
        bindLanguageScenario(result.scenario);
        setEditorFormat('language');
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
        setEditorFormat('json');
        setApplied(null);
        setFileNotice({ severity: 'success', message: `Imported ${file.name}. Ready for Start.` });
      },
    );
  }

  function hasUnappliedDocument() {
    return editedDocumentRef.current !== null || languageSourceRef.current !== languageBaselineRef.current;
  }

  function hasUnappliedActiveSource() {
    return editorModeRef.current === 'language' && (
      languageScenarioRef.current === null
      || languageScenarioRef.current !== scenarioRef.current
      || languageSourceRef.current !== languageBaselineRef.current
    );
  }

  // Programmatic loads choose their format only after success; text buffers stay intact.
  function setEditorFormat(mode: ScenarioEditorMode) {
    editorModeRef.current = mode;
    setEditorMode(mode);
  }

  function changeEditorFormat(mode: ScenarioEditorMode) {
    if (validationRef.current || pending || hasUnappliedDocument()) return;
    setEditorFormat(mode);
  }

  function changeDocument(document: string) {
    if (editorModeRef.current !== 'json') return;
    if (!scenarioRef.current || validationRef.current || pending) return;
    if (languageSourceRef.current !== languageBaselineRef.current) return;
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

  function changeLanguage(source: string) {
    if (editorModeRef.current !== 'language') return;
    if (validationRef.current || pending || editedDocumentRef.current !== null) return;
    languageSourceRef.current = source;
    setLanguageSource(source);
    setLanguageError(null);
    setFileNotice(null);
  }

  function discardLanguage() {
    if (validationRef.current || pending) return;
    languageSourceRef.current = languageBaselineRef.current;
    setLanguageSource(languageBaselineRef.current);
    setLanguageError(null);
    setFileNotice(null);
  }

  function bindLanguageScenario(definition: Scenario) {
    languageScenarioRef.current = definition;
    setLanguageScenario(definition);
  }

  function applyLanguage() {
    if (editorModeRef.current !== 'language') return;
    if (editedDocumentRef.current !== null) return;
    const source = languageSourceRef.current;
    setLanguageError(null);
    void validateAndApply(
      signal => validateLanguageDocument(source, scenarioApi, signal),
      result => {
        setEditorFormat('language');
        languageBaselineRef.current = source;
        setLanguageBaseline(source);
        bindLanguageScenario(result.scenario);
        setApplied(null);
        setFileNotice({ severity: 'success', message: 'Applied compiled source. Ready for Start.' });
      },
      failure => setLanguageError(failure instanceof LanguageError ? failure : null),
    );
  }

  async function importLanguage(file: File) {
    if (hasUnappliedDocument() || validationRef.current || pending) return;
    const controller = new AbortController();
    validationRef.current = controller;
    setValidating(true);
    setFileNotice(null);
    try {
      const loaded = await loadLanguageSource(file, controller.signal);
      if (controller.signal.aborted || validationRef.current !== controller) return;
      setEditorFormat('language');
      languageSourceRef.current = loaded.source;
      setLanguageSource(loaded.source);
      setLanguageError(loaded.diagnostic);
      setFileNotice({
        severity: loaded.diagnostic ? 'error' : 'success',
        message: `Loaded ${file.name} into the source draft. Compile and apply when ready.`,
      });
    } catch (failure) {
      if (!controller.signal.aborted && validationRef.current === controller) {
        setFileNotice({
          severity: 'error', message: failure instanceof Error ? failure.message : 'Could not load source.',
        });
      }
    } finally {
      if (validationRef.current === controller) {
        validationRef.current = null;
        if (!controller.signal.aborted) setValidating(false);
      }
    }
  }

  function exportLanguage() {
    if (validationRef.current || pending) return;
    try {
      downloadConfiguration(serializeLanguageSource(languageSourceRef.current), 'scenario.lb', 'text/plain');
      setFileNotice({ severity: 'success', message: 'Exported scenario.lb source.' });
    } catch (failure) {
      setFileNotice({ severity: 'error', message: failure instanceof Error ? failure.message : 'Could not export source.' });
    }
  }

  function applyDocument() {
    if (editorModeRef.current !== 'json') return;
    const document = editedDocumentRef.current;
    if (document === null) return;
    void validateAndApply(
      signal => validateEditorDocument(document, scenarioApi, signal),
      () => {
        setEditorFormat('json');
        setApplied(null);
        setFileNotice({ severity: 'success', message: 'Applied JSON document. Ready for Start.' });
      },
    );
  }

  function validateDraft() {
    const current = scenarioRef.current;
    if (!current || hasUnappliedDocument() || hasUnappliedActiveSource()) return;
    void validateAndApply(
      (signal) => scenarioApi.validate(current, signal),
      result => {
        if (languageScenarioRef.current === current) bindLanguageScenario(result.scenario);
        setFileNotice({
          severity: 'success', message: 'Draft validated. Effective configuration is available below.',
        });
      },
    );
  }

  function exportScenario() {
    const current = scenarioRef.current;
    if (!current || hasUnappliedDocument() || hasUnappliedActiveSource()) return;
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
      result => {
        // Browser side effects run only after the current-operation guard.
        if (languageScenarioRef.current === current) bindLanguageScenario(result.scenario);
        downloadConfiguration(document);
        setFileNotice({ severity: 'success', message: 'Exported scenario.json.' });
      },
    );
  }

  function startSimulation() {
    const current = scenarioRef.current;
    if (!current || !canStart || validationRef.current || hasUnappliedDocument() || hasUnappliedActiveSource()) return;
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
        unappliedInput={pendingEdits}
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
        jsonDirty={unappliedJSON}
        jsonBlocked={unappliedLanguage}
        pendingEdits={pendingEdits}
        editorMode={editorMode}
        onEditorModeChange={changeEditorFormat}
        languageEditor={configuration => (
          <Suspense fallback={<Alert severity="info">Loading source editor…</Alert>}>
            <ScenarioLanguageEditor
              active={editorMode === 'language'}
              configuration={configuration}
              appliedDocument={scenario ? appliedDocument : null}
              source={languageSource}
              dirty={unappliedLanguage}
              disabled={loading || pending || validating || unappliedJSON || editorMode !== 'language'}
              applied={languageApplied}
              diagnostic={languageError}
              onApply={applyLanguage}
              onImport={file => { void importLanguage(file); }}
              onExport={exportLanguage}
              onChange={changeLanguage}
              onDiscard={discardLanguage}
            />
          </Suspense>
        )}
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
function downloadConfiguration(contents: string, name = 'scenario.json', mime = 'application/json') {
  const blob = new Blob([contents], { type: `${mime};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  try {
    anchor.href = url;
    anchor.download = name;
    document.body.appendChild(anchor);
    anchor.click();
  } finally {
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}
