import type { Scenario, ScenarioState } from '../scenarios/types.ts';
import { MAX_SCENARIO_BYTES, serializeScenario } from '../scenarios/files.ts';
import { validateScenario } from '../scenarios/validation.ts';
import { FieldWriter } from './fields.ts';
import { LanguageError, locateValidationError } from './source.ts';
import type { SourceRange } from './source.ts';
import type { TimelineBlock } from './syntax.ts';
import { timeValue } from './values.ts';

type ApplyChanges = (state: ScenarioState, writer: FieldWriter, path: string) => void;
const utf8 = new TextEncoder();

// Build complete native states before Start; this never schedules runtime callbacks.
export class TimelineBuilder {
  private readonly scenario: Scenario;
  private readonly locations: Map<string, SourceRange>;
  private readonly fallback: SourceRange;
  private previousKey = '0';
  private previousOrigins: Map<string, SourceRange>;
  private documentBytes: number;

  constructor(scenario: Scenario, locations: Map<string, SourceRange>, fallback: SourceRange) {
    this.scenario = scenario;
    this.locations = locations;
    this.fallback = fallback;
    this.documentBytes = utf8.encode(JSON.stringify(scenario)).byteLength;
    const initialPath = 'states["0"]';
    this.previousOrigins = new Map([...locations]
      .filter(([path]) => path === initialPath || path.startsWith(`${initialPath}.`))
      .map(([path, origin]) => [path.slice(initialPath.length), origin]));
  }

  // Clone first and publish only after all changes succeed: a failed block adds no partial state.
  append(block: TimelineBlock, apply: ApplyChanges): void {
    const at = BigInt(timeValue(block.at, 1n));
    const duration = BigInt(this.scenario.fixed.execution.durationNanoseconds);
    if (at <= BigInt(this.previousKey) || at >= duration) {
      throw new LanguageError('At timestamps must increase strictly and precede the run end.', block.at.range);
    }
    if (block.commands.length === 0) {
      throw new LanguageError('An at block must contain at least one command.', block.range);
    }

    const key = at.toString();
    const path = `states[${JSON.stringify(key)}]`;
    const draft = structuredClone(this.scenario.states[this.previousKey]);
    const locations = new Map<string, SourceRange>();
    // Inherited settings retain their original source; this block replaces only changed origins.
    for (const [relativePath, origin] of this.previousOrigins) locations.set(path + relativePath, origin);
    const writer = new FieldWriter(locations);
    apply(draft, writer, path);

    // Count only the added entry, keeping size checks linear rather than serializing all prior states.
    const addedBytes = utf8.encode(JSON.stringify({ [key]: draft })).byteLength - 2 + 1;
    if (addedBytes > MAX_SCENARIO_BYTES - this.documentBytes) {
      throw new LanguageError('Compiled JSON exceeds 2 MiB; reduce resources or timeline states.', block.range);
    }
    this.scenario.states[key] = draft;
    const nextOrigins = new Map<string, SourceRange>();
    for (const [originPath, origin] of locations) {
      if (originPath === path || originPath.startsWith(`${path}.`)) {
        this.locations.set(originPath, origin);
        nextOrigins.set(originPath.slice(path.length), origin);
      }
    }
    this.previousOrigins = nextOrigins;
    this.previousKey = key;
    this.documentBytes += addedBytes;
  }

  // Validate the whole result, including unchanged resources and relationships across states.
  finish(): Scenario {
    try {
      const checked = validateScenario(this.scenario);
      serializeScenario(checked);
      return checked;
    } catch (error) {
      throw locateValidationError(error, this.locations, this.fallback);
    }
  }
}
