import { Alert, Button, Card, CardContent, CardHeader, Stack } from '@mui/material';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  applyScenarioField,
  scenarioFormValues,
  validateScenarioField,
} from '../scenarios/draft';
import type { ScenarioDraft, ScenarioField } from '../scenarios/draft';
import { readScenarioFile, serializeScenario } from '../scenarios/files';
import { scenarioPresets } from '../scenarios/presets';
import { scenarioApi } from '../scenarios/runtimeApi';
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
}

interface CountEdit {
  field: 'clients' | 'servers';
  baseline: Scenario;
}

type FieldErrors = Partial<Record<ScenarioField, string>>;

// One native document drives both presets and manual edits, independently of the active run.
function ExperimentSetup({
  connected,
  simulation,
  pending,
  currentRun,
  currentRunError,
  onRefreshCurrent,
  onStart,
}: ExperimentSetupProps) {
  const [defaults, setDefaults] = useState<Scenario | null>(null);
  const [scenario, setScenario] = useState<Scenario | null>(null);
  const [values, setValues] = useState<ScenarioDraft | null>(null);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [candidate, setCandidate] = useState<PresetCandidate | null>(null);
  const [applied, setApplied] = useState<AppliedPreset | null>(null);
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validation, setValidation] = useState<ValidatedScenario | null>(null);
  const [fileNotice, setFileNotice] = useState<ScenarioFileNotice | null>(null);
  const validationRef = useRef<AbortController | null>(null);
  const scenarioRef = useRef<Scenario | null>(null);
  const errorsRef = useRef<FieldErrors>({});
  const countEditRef = useRef<CountEdit | null>(null);

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
        setValues(scenarioFormValues(definition));
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

  useEffect(() => () => validationRef.current?.abort(), []);

  function choosePreset(id: string) {
    if (!defaults || validationRef.current || pending) return;
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
  ) {
    if (validationRef.current || pending) return;
    const controller = new AbortController();
    validationRef.current = controller;
    setValidating(true);
    setError(null);
    setFileNotice(null);

    try {
      const result = await load(controller.signal);
      const nextValues = scenarioFormValues(result.scenario);
      if (controller.signal.aborted || validationRef.current !== controller) return;

      countEditRef.current = null;
      setNativeScenario(result.scenario);
      setValues(nextValues);
      errorsRef.current = {};
      setFieldErrors({});
      setValidation(result);
      onApplied(result);
    } catch (failure) {
      if (!controller.signal.aborted) {
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
    if (!candidate) return;
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
    );
  }

  function importScenario(file: File) {
    void validateAndApply(
      async (signal) => {
        const document = await readScenarioFile(file);
        return scenarioApi.validateDocument(document, signal);
      },
      () => {
        setCandidate(null);
        setApplied(null);
        setFileNotice({ severity: 'success', message: `Imported ${file.name}. Ready for Start.` });
      },
    );
  }

  function validateDraft() {
    const current = scenarioRef.current;
    if (!current || Object.keys(errorsRef.current).length > 0) return;
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
    if (!current || Object.keys(errorsRef.current).length > 0) return;

    // Reuse an unchanged validated draft; edited settings are checked before saving.
    void validateAndApply(
      async (signal) => {
        const checked = validation?.scenario === current
          ? validation
          : await scenarioApi.validate(current, signal);
        const document = serializeScenario(checked.scenario);
        if (signal.aborted) throw signal.reason;

        const blob = new Blob([document], { type: 'application/json;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const anchor = createDownloadLink(url);
        anchor.click();
        anchor.remove();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
        return checked;
      },
      () => setFileNotice({ severity: 'success', message: 'Exported scenario.json.' }),
    );
  }

  function changeField(name: ScenarioField, raw: string) {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending) return;
    setValidation(null);
    setFileNotice(null);
    setValues((previous) => previous ? { ...previous, [name]: raw } : previous);
    setApplied((previous) => previous ? { ...previous, pristine: false } : null);
    let message = validateScenarioField(name, raw);

    if (name === 'clients' || name === 'servers') {
      if (countEditRef.current?.field !== name) {
        countEditRef.current = { field: name, baseline: current };
      }
    } else {
      countEditRef.current = null;
    }
    const baseline = countEditRef.current?.baseline ?? current;

    if (!message) {
      try {
        setNativeScenario(applyScenarioField(baseline, name, raw));
      } catch (failure) {
        message = failure instanceof Error ? failure.message : 'Cannot apply this setting.';
      }
    }

    const nextErrors = { ...errorsRef.current };
    if (message) nextErrors[name] = message;
    else delete nextErrors[name];
    errorsRef.current = nextErrors;
    setFieldErrors(nextErrors);
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
    const invalid = Object.keys(errorsRef.current).length > 0;
    if (!current || validationRef.current || pending || invalid) return;
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
        invalid={Object.keys(fieldErrors).length > 0}
        notice={fileNotice}
        onImport={importScenario}
        onExport={exportScenario}
        onValidate={validateDraft}
      />

      {scenario && values ? (
        <SimulationForm
          scenario={scenario}
          values={values}
          errors={fieldErrors}
          connected={connected}
          running={simulation?.status === 'running'}
          pending={pending}
          validating={validating}
          onChange={changeField}
          onFocus={focusField}
          onBlur={blurField}
          onStart={startSimulation}
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
function createDownloadLink(url: string): HTMLAnchorElement {
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = 'scenario.json';
  document.body.appendChild(anchor);
  return anchor;
}
