import { parseDuration, retainResourceIds, scenarioStateTimes, selectedScenarioState } from './draft.ts';
import type { Scenario } from './types.ts';
import { validateScenario } from './validation.ts';
import { INT64_MAX } from './values.ts';

export type TimelineEdit =
  | { type: 'add' | 'copy' | 'move'; seconds: string }
  | { type: 'delete' };

export interface TimelineEditResult {
  scenario: Scenario;
  selectedAt: string;
}

// Seconds are parsed digit by digit; the JSON key always contains exact nanoseconds.
export function parseTimelineTimestamp(seconds: string): string {
  const at = parseDuration(seconds, 9);
  if (at > INT64_MAX) throw new Error('State time exceeds the supported int64 nanosecond range.');
  return at.toString();
}

// Commit a complete valid timeline or leave the caller's document and selection intact.
export function editScenarioTimeline(
  scenario: Scenario, selectedAt: string, edit: TimelineEdit,
): TimelineEditResult {
  selectedScenarioState(scenario, selectedAt);
  if ((edit.type === 'delete' || edit.type === 'move') && selectedAt === '0') {
    throw new Error('The initial state at 0 seconds cannot be moved or deleted.');
  }

  const times = scenarioStateTimes(scenario);
  const next = structuredClone(scenario);
  let nextAt: string;
  if (edit.type === 'delete') {
    delete next.states[selectedAt];
    nextAt = times[times.indexOf(selectedAt) - 1];
  } else {
    nextAt = parseTimelineTimestamp(edit.seconds);
    if (BigInt(nextAt) >= BigInt(scenario.fixed.execution.durationNanoseconds)) {
      throw new Error('State time must be strictly below the simulation duration.');
    }
    if (Object.prototype.hasOwnProperty.call(scenario.states, nextAt)) {
      throw new Error(`A state already exists at ${nextAt} nanoseconds. Choose a different time.`);
    }
    const preceding = times.filter(at => BigInt(at) <= BigInt(nextAt));
    const sourceAt = edit.type === 'add' ? preceding[preceding.length - 1] ?? '0' : selectedAt;
    next.states[nextAt] = structuredClone(selectedScenarioState(scenario, sourceAt));
    if (edit.type === 'move') delete next.states[selectedAt];
  }

  try {
    return { scenario: retainResourceIds(validateScenario(next), scenario), selectedAt: nextAt };
  } catch (failure) {
    const detail = failure instanceof Error ? failure.message : 'The resulting timeline is invalid.';
    let explanation = '';
    if (detail.includes('new group must start at or after its insertion time')) {
      explanation = ' This would place a group’s first appearance after its fixed start time. Keep its first state at or before that start time.';
    } else if (/\.overrides\[\d+\]\.index: must identify a client in this group/.test(detail)) {
      explanation = ' This would remove an earlier client budget needed by a later override. Keep the earlier budget or edit that override first.';
    }
    throw new Error(`Cannot ${edit.type} state. ${detail}${explanation}`);
  }
}
