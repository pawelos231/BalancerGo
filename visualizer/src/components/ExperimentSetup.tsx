import { Alert, Button, Card, CardContent, CardHeader, Stack } from '@mui/material';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  applyScenarioField,
  scenarioFormValues,
  validateScenarioField,
} from '../scenarios/draft';
import type { ScenarioDraft, ScenarioField } from '../scenarios/draft';
import { scenarioPresets } from '../scenarios/presets';
import { scenarioApi } from '../scenarios/runtimeApi';
import type { CurrentSimulation, Scenario } from '../scenarios/types';
import type { SimulationMetadata } from '../types';
import ScenarioLibrary from './ScenarioLibrary';
import type { AppliedPreset, PresetCandidate } from './ScenarioLibrary';
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
      setNativeScenario(definition);
      setValues(scenarioFormValues(definition));
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

  async function validatePreset() {
    if (!candidate || validationRef.current || pending) return;
    const controller = new AbortController();
    validationRef.current = controller;
    setValidating(true);
    setError(null);

    try {
      const result = await scenarioApi.validate(candidate.scenario, controller.signal);
      if (controller.signal.aborted) return;
      countEditRef.current = null;
      setNativeScenario(result.scenario);
      setValues(scenarioFormValues(result.scenario));
      errorsRef.current = {};
      setFieldErrors({});
      setApplied({
        id: candidate.id,
        title: candidate.title,
        summary: result.summary,
        pristine: true,
      });
    } catch (failure) {
      if (!controller.signal.aborted) {
        setError(failure instanceof Error ? failure.message : 'Scenario validation failed.');
      }
    } finally {
      if (!controller.signal.aborted) {
        validationRef.current = null;
        setValidating(false);
      }
    }
  }

  function changeField(name: ScenarioField, raw: string) {
    const current = scenarioRef.current;
    if (!current || validationRef.current || pending) return;
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
