import type { ExactInteger, ScenarioClientGroup } from '../scenarios/types.ts';
import { clientFields, clientStreamFields } from './bindings.ts';
import type { FieldWriter } from './fields.ts';
import { endpointName, prefixName } from './identity.ts';
import { selectEndpointProfile, writeEndpointNetwork } from './network.ts';
import type { Declaration } from './syntax.ts';
import { LanguageError } from './source.ts';
import { exactInteger } from './values.ts';

// Keep a compact generator: count 50,000 never creates 50,000 compiler-side clients.
export function compileClientGroup(
  declaration: Declaration,
  writer: FieldWriter,
  path: string,
  startAt: ExactInteger = 0,
): ScenarioClientGroup {
  writer.locations.set(path, declaration.range);
  writer.locations.set(`${path}.id`, declaration.argument!.range);
  const group: ScenarioClientGroup = {
    id: prefixName(declaration.argument!), count: 1,
    startAtNanoseconds: startAt, clientGapNanoseconds: 0,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: {}, overrides: [],
  };
  selectEndpointProfile(declaration.profile, group.network, writer, `${path}.network`);
  const other = writeEndpointNetwork(declaration.properties, group.network, writer, `${path}.network`);
  writer.apply(other, clientFields, group, path);
  return group;
}

// regular-1 becomes group regular, index 1; it never creates a new runtime client ID.
export function compileClientOverrides(
  declarations: Declaration[],
  groups: ScenarioClientGroup[],
  writer: FieldWriter,
  rootPath: string,
): void {
  const byName = new Map(groups.map((group, index) => [group.id, { group, index }]));
  const used = new Set<string>();

  for (const declaration of declarations) {
    const name = endpointName(declaration.argument!);
    const dash = name.lastIndexOf('-');
    const prefix = name.slice(0, dash);
    const index = BigInt(name.slice(dash + 1));
    const entry = byName.get(prefix);
    if (!entry || index >= BigInt(entry.group.count)) {
      throw new LanguageError(`Client ${name} is outside its declared group budget.`, declaration.argument!.range);
    }
    if (used.has(name)) {
      throw new LanguageError(`Duplicate client override ${name}.`, declaration.argument!.range);
    }
    used.add(name);

    const overrides = entry.group.overrides!;
    const path = `${rootPath}.clientGroups[${entry.index}].overrides[${overrides.length}]`;
    const override = {
      index: exactInteger(index),
      network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
      stream: {},
    };
    selectEndpointProfile(declaration.profile, override.network, writer, `${path}.network`);
    const other = writeEndpointNetwork(declaration.properties, override.network, writer, `${path}.network`);
    writer.apply(other, clientStreamFields, override, path);
    writer.locations.set(`${path}.index`, declaration.argument!.range);
    overrides.push(override);
  }
}
