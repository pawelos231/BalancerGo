import type { ScenarioBackendState } from '../scenarios/types.ts';
import { backendFields } from './bindings.ts';
import { defaultHost } from './defaults.ts';
import { FieldWriter } from './fields.ts';
import { backendAddresses, endpointName, prefixName } from './identity.ts';
import { selectEndpointProfile, writeEndpointNetwork } from './network.ts';
import { LanguageError } from './source.ts';
import type { Command, Declaration, Program, Property } from './syntax.ts';
import { countValue } from './values.ts';

// Expand only backend templates; clients remain compact runtime generators.
export function expandBackends(declarations: Declaration[], capacity = 10_000): Declaration[] {
  const expanded: Declaration[] = [];
  for (const declaration of declarations) {
    if (declaration.keyword.text === 'backend') {
      if (expanded.length === capacity) {
        throw new LanguageError('At most 10,000 backends are supported.', declaration.range);
      }
      expanded.push(declaration);
      continue;
    }

    const prefix = prefixName(declaration.argument!);
    const metadata: Property[] = [];
    const properties: Property[] = [];
    for (const property of declaration.properties) {
      const isMetadata = property.key.text === 'count' || property.key.text === 'from';
      (isMetadata ? metadata : properties).push(property);
    }
    const settings = { count: 0 as number | string, from: 0 as number | string };
    const writer = new FieldWriter();
    writer.apply(metadata, {
      count: { path: 'count', read: token => countValue(token, 1n, 10_000n) },
      from: { path: 'from', read: token => countValue(token) },
    }, settings, 'backends');
    if (!writer.hasWritten('backends.count')) {
      throw new LanguageError('Bulk backends require a count setting.', declaration.range);
    }
    const count = Number(settings.count);
    if (count > capacity - expanded.length) {
      throw new LanguageError('At most 10,000 backends are supported.', declaration.range);
    }

    // BigInt also keeps a large starting suffix exact while incrementing it.
    const firstIndex = BigInt(settings.from);
    for (let offset = 0; offset < count; offset++) {
      const argument = { ...declaration.argument!, text: `${prefix}-${firstIndex + BigInt(offset)}` };
      endpointName(argument);
      expanded.push({ ...declaration, argument, properties });
    }
  }
  return expanded;
}

export interface BackendPlan {
  initial: Declaration[];
  declarations: Declaration[];
  additions: ReadonlyMap<Command, Declaration[]>;
  addresses: ReadonlyMap<string, string>;
}

// Reserve identities for the whole source before emitting even the initial state.
export function planBackends(program: Program): BackendPlan {
  const initial = expandBackends(program.declarations.filter(declaration => {
    return declaration.keyword.text === 'backend' || declaration.keyword.text === 'backends';
  }));
  const declarations = [...initial];
  const additions = new Map<Command, Declaration[]>();
  for (const block of program.timeline) {
    for (const command of block.commands) {
      if (command.kind !== 'add' || !['backend', 'backends'].includes(command.declaration.keyword.text)) continue;
      const expanded = expandBackends([command.declaration], 10_000 - declarations.length);
      declarations.push(...expanded);
      additions.set(command, expanded);
    }
  }

  const ids = new Set<string>();
  for (const declaration of declarations) {
    const id = endpointName(declaration.argument!);
    if (ids.has(id)) throw new LanguageError(`Duplicate backend ${id}.`, declaration.argument!.range);
    ids.add(id);
  }
  return { initial, declarations, additions, addresses: backendAddresses([...ids]) };
}

// One expanded backend uses the same construction path as an explicitly declared one.
export function compileBackend(
  declaration: Declaration,
  address: string,
  writer: FieldWriter,
  path: string,
): ScenarioBackendState {
  writer.locations.set(path, declaration.range);
  writer.locations.set(`${path}.id`, declaration.argument!.range);
  const backend: ScenarioBackendState = {
    id: endpointName(declaration.argument!), address, port: 80,
    host: defaultHost(),
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
  };
  selectEndpointProfile(declaration.profile, backend.network, writer, `${path}.network`);
  const other = writeEndpointNetwork(declaration.properties, backend.network, writer, `${path}.network`);
  writer.apply(other, backendFields, backend, path);
  return backend;
}
