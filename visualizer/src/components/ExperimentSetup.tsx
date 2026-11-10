import { Alert, Button, Card, CardContent, CardHeader, Stack } from '@mui/material';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { rebaseScenarioCount, retainResourceIds, scenarioFormValues } from '../scenarios/draft';
import type { ScenarioDraft, ScenarioField } from '../scenarios/draft';
import { editScenarioForm, retryScenarioForm } from '../scenarios/formState';
import type { FieldErrors, ScenarioFormState } from '../scenarios/formState';
import {
  applyHostToAllBackends, editHostField, emptyHostEdits, retainHostEdits, retryHostEdits,
} from '../scenarios/host';
import type { HostEdits, HostField, HostTarget, HostValues } from '../scenarios/host';
import { formatScenarioDocument, readScenarioFile, serializeScenario } from '../scenarios/files';
import { validateEditorDocument } from '../scenarios/editor';
import { scenarioPresets } from '../scenarios/presets';
import { scenarioApi } from '../scenarios/runtimeApi';
import { validateScenario } from '../scenarios/validation';
import { editScenarioTimeline } from '../scenarios/timelineEdits';
import type { TimelineEdit } from '../scenarios/timelineEdits';
import type { CurrentSimulation, Scenario, ValidatedScenario } from '../scenarios/types';
import type { SimulationMetadata } from '../types';
import ScenarioLibrary from './ScenarioLibrary';
import type { AppliedPreset, PresetCandidate } from './ScenarioLibrary';
import ScenarioFiles from './ScenarioFiles';
import type { ScenarioFileNotice } from './ScenarioFiles';
import ScenarioRunDetails from './ScenarioRunDetails';
import SimulationForm from './SimulationForm';

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

interface CountEdit {
  field: 'clients' | 'servers';
  baseline: Scenario;
}

// One native document drives both presets and manual edits, independently of the active run.
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
  const [selectedAt, setSelectedAt] = useState('0');
  const selectedAtRef = useRef('0');
  const [defaults, setDefaults] = useState<Scenario | null>(null);
  const [scenario, setScenario] = useState<Scenario | null>(null);
  const [values, setValues] = useState<ScenarioDraft | null>(null);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [hostEdits, setHostEdits] = useState<HostEdits>(emptyHostEdits);
  const [hostRevision, setHostRevision] = useState(0);
  const [stagedHosts, setStagedHosts] = useState(false);
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [candidate, setCandidate] = useState<PresetCandidate | null>(null);
  const [applied, setApplied] = useState<AppliedPreset | null>(null);
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validation, setValidation] = useState<ValidatedScenario | null>(null);
  const [fileNotice, setFileNotice] = useState<ScenarioFileNotice | null>(null);
  const [editedDocument, setEditedDocument] = useState<string | null>(null);
  const editedDocumentRef = useRef<string | null>(null);
  const validationRef = useRef<AbortController | null>(null);
  const scenarioRef = useRef<Scenario | null>(null);
  const valuesRef = useRef<ScenarioDraft | null>(null);
  const errorsRef = useRef<FieldErrors>({});
  const hostEditsRef = useRef<HostEdits>(emptyHostEdits());
  const stagedHostsRef = useRef(false);
  const countEditRef = useRef<CountEdit | null>(null);
  const appliedDocument = useMemo(() => scenario ? formatScenarioDocument(scenario) : '', [scenario]);
  const unappliedDocument = editedDocument !== null;
  const unappliedForm = Object.keys(fieldErrors).length > 0
    || Object.keys(hostEdits.errors).length > 0 || stagedHosts;

  const changeStagedHosts = useCallback((staged: boolean) => {
    if (staged && !stagedHostsRef.current) setFileNotice(null);
    stagedHostsRef.current = staged;
    setStagedHosts(staged);
  }, []);

  const setNativeScenario = useCallback((definition: Scenario) => {
    scenarioRef.current = definition;
    setScenario(definition);
  }, []);

  const setFormValues = useCallback((draft: ScenarioDraft) => {
    valuesRef.current = draft;
    setValues(draft);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    scenarioApi.defaults(controller.signal).then((definition) => {
      if (controller.signal.aborted) return;
      setDefaults(definition);
      // A late defaults response must not overwrite an already imported draft.
      if (!scenarioRef.current) {
        setNativeScenario(definition);
        setFormValues(scenarioFormValues(definition));
      }
      setError(null);
    }).catch((failure: unknown) => {
      if (controller.signal.aborted) return;
      setError(failure instanceof Error ? failure.message : 'Cannot load scenario defaults.');
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });

    return () => controller.abort();
  }, [reload, setNativeScenario, setFormValues]);

  useEffect(() => () => {
    validationRef.current?.abort();
    validationRef.current = null;
  }, []);

  // Rejected text remains in the form; only a complete valid document updates the plan.
  useEffect(() => {
    if (!scenario) return;
    if (unappliedDocument) {
      onPreviewChange(null, 'JSON edits have not been applied. Apply or discard them to update the planned topology.');
      return;
    }
    if (Object.keys(fieldErrors).length > 0 || Object.keys(hostEdits.errors).length > 0) {
      onPreviewChange(null, 'The form has invalid, unapplied input. Fix the highlighted fields to update the planned topology.');
      return;
    }
    try {
      const notice = stagedHosts
        ? 'Common backend CPU inputs have not been applied. Use Apply to all backends on the form to update those settings.'
        : null;
      onPreviewChange(validateScenario(scenario), notice);
    } catch (failure) {
      onPreviewChange(null, failure instanceof Error ? failure.message : 'The draft is not a valid complete scenario.');
    }
  }, [scenario, fieldErrors, hostEdits.errors, stagedHosts, unappliedDocument, onPreviewChange]);

  function choosePreset(id: string) {
    if (!defaults || validationRef.current || pending
      || hasUnappliedDocument() || hasUnappliedForm()) return;
    const preset = scenarioPresets.find((entry) => entry.id === id);
    if (!preset) return;
    setCandidate({ id, title: preset.title, scenario: preset.build(defaults) });
    setError(null);
  }

  // Lock every draft action until reading and server validation have both finished.
  // The old document and form stay intact unless the complete operation succeeds.
  async function validateAndApply(
    load: (signal: AbortSignal) => Promise<ValidatedScenario>,
    onApplied: (result: ValidatedScenario) => void,
    replaceDocument = false,
  ) {
    if (validationRef.current || pending || hasUnappliedForm()) return;
    const controller = new AbortController();
    validationRef.current = controller;
    setValidating(true);
    setError(null);
    setFileNotice(null);

    try {
      const result = await load(controller.signal);
      if (controller.signal.aborted || validationRef.current !== controller) return;
      const nextAt = result.scenario.states[selectedAtRef.current] ? selectedAtRef.current : '0';

      if (!replaceDocument && scenarioRef.current) retainResourceIds(result.scenario, scenarioRef.current);
      resetFormInputs(result.scenario, nextAt);
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

  function validatePreset() {
    if (!candidate || hasUnappliedDocument() || hasUnappliedForm()) return;
    const selected = candidate;
    void validateAndApply(
      (signal) => scenarioApi.validate(selected.scenario, signal),
      (result) => {
        setApplied({
          id: selected.id,
          title: selected.title,
          summary: result.summary,
          pristine: true,
        });
        setFileNotice({ severity: 'success', message: `Loaded preset: ${selected.title}.` });
      },
      true,
    );
  }

  function importScenario(file: File) {
    if (hasUnappliedDocument() || hasUnappliedForm()) return;
    void validateAndApply(
      async (signal) => {
        const document = await readScenarioFile(file);
        signal.throwIfAborted();
        return scenarioApi.validateDocument(document, signal);
      },
      () => {
        setCandidate(null);
        setApplied(null);
        setFileNotice({ severity: 'success', message: `Imported ${file.name}. Ready for Start.` });
      },
      true,
    );
  }

  function hasUnappliedDocument() {
    return editedDocumentRef.current !== null;
  }

  function changeDocument(document: string) {
    if (!scenarioRef.current || validationRef.current || pending || hasUnappliedForm()) return;
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
        setCandidate(null);
        setApplied(null);
        setFileNotice({ severity: 'success', message: 'Applied JSON document. Ready for Start.' });
      },
      true,
    );
  }

  function validateDraft() {
    const current = scenarioRef.current;
    if (!current || hasUnappliedForm() || hasUnappliedDocument()) return;
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
    if (!current || hasUnappliedForm() || hasUnappliedDocument()) return;
    let document = '';

    // Reuse an unchanged validated draft; edited settings are checked before saving.
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

  function changeField(name: ScenarioField, raw: string) {
    const current = scenarioRef.current;
    const requested = valuesRef.current;
    if (!current || !requested || validationRef.current || pending || hasUnappliedDocument()) return;
    setFileNotice(null);

    if (name === 'clients' || name === 'servers') {
      if (countEditRef.current?.field !== name) {
        countEditRef.current = { field: name, baseline: current };
      }
    } else {
      countEditRef.current = null;
    }
    const baseline = countEditRef.current?.baseline ?? current;

    const next = editScenarioForm(
      { scenario: current, values: requested, errors: errorsRef.current, selectedAt: selectedAtRef.current },
      name, raw, baseline,
    );
    const hosts = retainHostEdits(
      next.scenario, hostEditsRef.current, false, selectedAtRef.current,
    );
    commitFormInputs(next, hosts);
  }

  function selectState(at: string) {
    const current = scenarioRef.current;
    if (!current?.states[at] || validationRef.current || pending || hasUnappliedDocument() || timelineInputNotice()) return;
    resetFormInputs(current, at);
  }

  function timelineInputNotice(): string | undefined {
    if (hasInvalidSettings()) {
      return 'Correct or discard the highlighted form and host inputs before selecting or changing states.';
    }
    if (stagedHostsRef.current) {
      return 'Apply or discard the common backend CPU inputs before selecting or changing states.';
    }
  }

  function changeTimeline(edit: TimelineEdit): string | undefined {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending) return 'Wait for the current draft action to finish.';
    if (hasUnappliedDocument()) return 'Apply or discard the JSON edits before changing states.';
    const blocked = timelineInputNotice();
    if (blocked) return blocked;

    try {
      const next = editScenarioTimeline(current, selectedAtRef.current, edit);
      resetFormInputs(next.scenario, next.selectedAt);
      setNativeScenario(next.scenario);
      markDraftEdited();
    } catch (failure) {
      return failure instanceof Error ? failure.message : 'Cannot change the configuration timeline.';
    }
  }

  function hasInvalidSettings() {
    return Object.keys(errorsRef.current).length > 0 || Object.keys(hostEditsRef.current.errors).length > 0;
  }

  function hasUnappliedForm() {
    return hasInvalidSettings() || stagedHostsRef.current;
  }

  function markDraftEdited() {
    setValidation(null);
    setFileNotice(null);
    setApplied(previous => previous ? { ...previous, pristine: false } : null);
  }

  function resetFormInputs(definition: Scenario, at = selectedAtRef.current) {
    const projected = scenarioFormValues(definition, at);
    countEditRef.current = null;
    selectedAtRef.current = at;
    setSelectedAt(at);
    setFormValues(projected);
    errorsRef.current = {};
    setFieldErrors({});
    hostEditsRef.current = emptyHostEdits();
    setHostEdits(hostEditsRef.current);
    changeStagedHosts(false);
    // Clear local common controls only at an explicit reset or state replacement.
    setHostRevision(revision => revision + 1);
  }

  function discardFormInputs() {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending || hasUnappliedDocument()) return;
    resetFormInputs(current);
    setFileNotice(null);
  }

  // Form and host constraints can depend on each other. Retry only rejected input;
  // each successful retry removes an error and keeps the whole document valid.
  function commitFormInputs(form: ScenarioFormState, hosts: HostEdits) {
    let next = form;
    let edits = hosts;
    let previousErrors: number;
    do {
      previousErrors = Object.keys(next.errors).length + Object.keys(edits.errors).length;
      const retried = retryHostEdits(next.scenario, edits, selectedAtRef.current);
      edits = retried.edits;
      next = retryScenarioForm(
        { ...next, scenario: retried.scenario }, countEditRef.current?.baseline,
      );
    } while (Object.keys(next.errors).length + Object.keys(edits.errors).length < previousErrors);

    const countEdit = countEditRef.current;
    if (countEdit) {
      countEdit.baseline = rebaseScenarioCount(
        countEdit.baseline, next.scenario, countEdit.field, selectedAtRef.current,
      );
    }

    if (next.scenario !== scenarioRef.current) {
      setNativeScenario(next.scenario);
      markDraftEdited();
    }
    setFormValues(next.values);
    errorsRef.current = next.errors;
    setFieldErrors(next.errors);
    hostEditsRef.current = edits;
    setHostEdits(edits);
  }

  // Host edits use the same native document while preserving unrelated rejected input.
  function updateHosts(next: Scenario, edits: HostEdits) {
    countEditRef.current = null;
    setFileNotice(null);
    const projected = scenarioFormValues(next, selectedAtRef.current);
    for (const name of Object.keys(errorsRef.current) as ScenarioField[]) {
      projected[name] = valuesRef.current?.[name] ?? projected[name];
    }
    commitFormInputs({
      scenario: next, values: projected, errors: errorsRef.current,
      selectedAt: selectedAtRef.current,
    }, edits);
  }

  function changeHostField(target: HostTarget, field: HostField, raw: string) {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending || hasUnappliedDocument()) return;
    const next = editHostField(current, hostEditsRef.current, target, field, raw, selectedAtRef.current);
    updateHosts(next.scenario, next.edits);
  }

  function applyHostToAll(values: HostValues): string | undefined {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending || hasUnappliedDocument()) {
      return 'Wait for the current draft action to finish.';
    }
    try {
      const next = applyHostToAllBackends(current, values, selectedAtRef.current);
      const edits = retainHostEdits(next, hostEditsRef.current, true, selectedAtRef.current);
      updateHosts(next, edits);
    } catch (failure) {
      return failure instanceof Error ? failure.message : 'Cannot apply common backend CPU settings.';
    }
  }

  // Intermediate count digits must not erase group proportions or custom backends.
  function focusField(name: ScenarioField) {
    const current = scenarioRef.current;
    countEditRef.current = current && (name === 'clients' || name === 'servers')
      ? { field: name, baseline: current }
      : null;
  }

  function blurField(name: ScenarioField) {
    if (countEditRef.current?.field === name) countEditRef.current = null;
  }

  function startSimulation() {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending
      || hasUnappliedForm() || hasUnappliedDocument()) return;
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
        unappliedInput={unappliedDocument || unappliedForm}
        candidate={candidate}
        applied={applied}
        error={error}
        onChoose={choosePreset}
        onValidate={validatePreset}
        onRetryDefaults={reloadDefaults}
      />

      <ScenarioFiles
        scenario={scenario}
        validation={validation}
        busy={loading || pending || validating}
        checking={validating}
        invalid={Object.keys(fieldErrors).length > 0 || Object.keys(hostEdits.errors).length > 0}
        stagedHosts={stagedHosts}
        notice={fileNotice}
        document={editedDocument ?? appliedDocument}
        unapplied={unappliedDocument}
        onEditDocument={changeDocument}
        onApplyDocument={applyDocument}
        onDiscardDocument={discardDocument}
        onImport={importScenario}
        onExport={exportScenario}
        onValidate={validateDraft}
      />

      {scenario && values ? (
        <SimulationForm
          scenario={scenario}
          selectedAt={selectedAt}
          onSelectState={selectState}
          onEditTimeline={changeTimeline}
          timelineBlockedReason={timelineInputNotice()}
          values={values}
          errors={fieldErrors}
          connected={connected}
          running={simulation?.status === 'running'}
          pending={pending}
          validating={validating}
          unappliedDocument={unappliedDocument}
          unappliedForm={unappliedForm}
          onDiscardFormInputs={discardFormInputs}
          onChange={changeField}
          onFocus={focusField}
          onBlur={blurField}
          onStart={startSimulation}
          hostEdits={hostEdits}
          hostRevision={hostRevision}
          onHostChange={changeHostField}
          onApplyHostToAll={applyHostToAll}
          onStagedHostChange={changeStagedHosts}
        />
      ) : (
        <Alert severity="info">Waiting for complete scenario defaults from the server…</Alert>
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
