import { applyScenarioField, scenarioFormValues } from './draft.ts';
import type { ScenarioDraft, ScenarioField } from './draft.ts';
import type { Scenario } from './types.ts';

export type FieldErrors = Partial<Record<ScenarioField, string>>;

export interface ScenarioFormState {
  scenario: Scenario;
  values: ScenarioDraft;
  errors: FieldErrors;
}

const recoveryFields: readonly ScenarioField[] = [
  'sackEnabled', 'ECNenabled', 'fastRetransmit', 'congestionControl',
  'maximumCongestionWindowSegments',
  'mssBytes', 'initialCongestionWindowSegments', 'initialSlowStartThresholdBytes',
];

// Keep rejected input visible, then retry it when a related setting changes.
// Example: SACK enabled under Tahoe becomes applicable after selecting Reno.
export function editScenarioForm(
  state: ScenarioFormState,
  field: ScenarioField,
  raw: string,
  baseline = state.scenario,
): ScenarioFormState {
  let scenario = state.scenario;
  const values = { ...state.values, [field]: raw };
  const errors = { ...state.errors };

  function apply(name: ScenarioField, source: Scenario): boolean {
    try {
      scenario = applyScenarioField(source, name, values[name]);
      delete errors[name];

      // Mode none disables fast retransmit; preserve an explicitly pending edit.
      if (name === 'congestionControl' && !('fastRetransmit' in errors)) {
        values.fastRetransmit = scenarioFormValues(scenario).fastRetransmit;
      }
      if (name === 'queuePolicy' || name === 'ingressQueuePolicy') {
        const projected = scenarioFormValues(scenario);
        const dependents = name === 'queuePolicy'
          ? ['congestionThresholdPercent', 'congestionSignalProbability'] as const
          : ['ingressCongestionThresholdPercent', 'ingressCongestionSignalProbability'] as const;
        for (const dependent of dependents) {
          if (!(dependent in errors)) values[dependent] = projected[dependent];
        }
      }
      return true;
    } catch (failure) {
      errors[name] = failure instanceof Error ? failure.message : 'Cannot apply this setting.';
      return false;
    }
  }

  apply(field, baseline);
  if (recoveryFields.includes(field)) {
    let appliedPendingEdit: boolean;
    do {
      appliedPendingEdit = false;
      for (const name of recoveryFields) {
        if (name in errors && apply(name, scenario)) appliedPendingEdit = true;
      }
      // Applying fast retransmit can make a previously rejected SACK choice valid.
    } while (appliedPendingEdit);
  }

  return { scenario, values, errors };
}
