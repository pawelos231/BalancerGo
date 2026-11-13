import type { ScenarioBackendState } from '../scenarios/types.ts';
import { hostFields } from './bindings.ts';
import { defaultHost } from './defaults.ts';
import { FieldWriter } from './fields.ts';
import { endpointName, prefixName } from './identity.ts';
import { selectEndpointProfile, writeEndpointNetwork } from './network.ts';
import { LanguageError } from './source.ts';
import type { Declaration, Property } from './syntax.ts';
import { countValue, textValue } from './values.ts';

// Expand only backend templates; clients remain compact runtime generators.
export function expandBackends(declarations: Declaration[]): Declaration[] {
  const expanded: Declaration[] = [];
  for (const declaration of declarations) {
    if (declaration.keyword.text === 'backend') {
      if (expanded.length === 10_000) {
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
    if (count > 10_000 - expanded.length) {
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
  writer.apply(other, {
    name: { path: 'name', read: textValue },
    host: { path: 'host', fields: hostFields },
  }, backend, path);
  return backend;
}
