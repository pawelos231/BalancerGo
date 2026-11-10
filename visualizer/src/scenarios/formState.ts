import { applyScenarioField, rebaseScenarioCount, scenarioFormValues } from './draft.ts';
import type { ScenarioDraft, ScenarioField } from './draft.ts';
import type { Scenario } from './types.ts';
import { validateScenario } from './validation.ts';

export type FieldErrors = Partial<Record<ScenarioField, string>>;

export interface ScenarioFormState {
  scenario: Scenario;
  values: ScenarioDraft;
  errors: FieldErrors;
  selectedAt?: string;
}

// Project only after pending input has been applied or explicitly discarded.
export function selectScenarioFormState(scenario: Scenario, selectedAt = '0'): ScenarioFormState {
  return { scenario, selectedAt, values: scenarioFormValues(scenario, selectedAt), errors: {} };
}

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

  function apply(name: ScenarioField): boolean {
    try {
      const source = name === 'clients' || name === 'servers'
        ? rebaseScenarioCount(baseline, scenario, name, state.selectedAt ?? '0')
        : scenario;
      const candidate = applyScenarioField(
        source, name, values[name], state.selectedAt ?? '0', state.scenario,
      );
      // Check every state without replacing native omissions or ID reservations.
      validateScenario(candidate);
      scenario = candidate;
      delete errors[name];

      // Mode none disables fast retransmit; preserve an explicitly pending edit.
      if (name === 'congestionControl' && !('fastRetransmit' in errors)) {
        values.fastRetransmit = scenarioFormValues(scenario, state.selectedAt ?? '0').fastRetransmit;
      }
      if (name === 'queuePolicy' || name === 'ingressQueuePolicy') {
        const projected = scenarioFormValues(scenario, state.selectedAt ?? '0');
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

  apply(field);
  let appliedPendingEdit: boolean;
  do {
    appliedPendingEdit = false;
    for (const name of Object.keys(errors) as ScenarioField[]) {
      if (apply(name)) appliedPendingEdit = true;
    }
    // A corrected dependency can make another pending choice applicable.
  } while (appliedPendingEdit);

  return { scenario, values, errors, selectedAt: state.selectedAt };
}

// Host edits can also resolve a pending form constraint, such as duration.
export function retryScenarioForm(
  state: ScenarioFormState, baseline = state.scenario,
): ScenarioFormState {
  const field = Object.keys(state.errors)[0] as ScenarioField | undefined;
  return field ? editScenarioForm(state, field, state.values[field], baseline) : state;
}
