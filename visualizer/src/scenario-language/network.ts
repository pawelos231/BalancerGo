import type { ScenarioEndpointNetwork, ScenarioNetworkConfig } from '../scenarios/types.ts';
import { FieldWriter } from './fields.ts';
import { linkFields, linkPatchFields } from './bindings.ts';
import type { Declaration, Property } from './syntax.ts';
import type { Token } from './lexer.ts';
import { prefixName } from './identity.ts';
import { LanguageError } from './source.ts';

const selectionFields = { use: { path: 'profile', read: prefixName } };

// Apply shared endpoint settings first, then directional overrides, regardless of source order.
export function writeEndpointNetwork(
  properties: Property[],
  network: ScenarioEndpointNetwork,
  writer: FieldWriter,
  path: string,
): Property[] {
  const shared: Property[] = [];
  const directions: Property[] = [];
  const selection: Property[] = [];
  const other: Property[] = [];
  for (const property of properties) {
    const name = property.key.text;
    if (Object.prototype.hasOwnProperty.call(linkFields, name)) shared.push(property);
    else if (name === 'to-lb' || name === 'from-lb') directions.push(property);
    else if (name === 'use') selection.push(property);
    else other.push(property);
  }

  writer.apply(selection, selectionFields, network, path);
  writer.apply(shared, linkPatchFields, network.toLoadBalancer, `${path}.toLoadBalancer`);
  writer.apply(shared, linkPatchFields, network.fromLoadBalancer, `${path}.fromLoadBalancer`);

  // A directional setting intentionally replaces its shared shorthand, rather than conflicting.
  const directionalWriter = new FieldWriter(writer.locations);
  directionalWriter.apply(directions, {
    'to-lb': { path: 'toLoadBalancer', fields: linkPatchFields },
    'from-lb': { path: 'fromLoadBalancer', fields: linkPatchFields },
  }, network, path);
  return other;
}

// A header's "use mobile" and a body selection share one native destination and conflict checks.
export function selectEndpointProfile(
  profile: Token | undefined,
  network: ScenarioEndpointNetwork,
  writer: FieldWriter,
  path: string,
): void {
  if (!profile) return;
  const key = { ...profile, text: 'use' };
  writer.apply([{ key, value: profile }], selectionFields, network, path);
}

// Collect all definitions before endpoints, so forward references are independent of source order.
export function compileProfiles(
  declarations: Declaration[],
  writer: FieldWriter,
): ScenarioNetworkConfig['profiles'] {
  const used = new Set<string>();
  return declarations.map((declaration, index) => {
    const id = prefixName(declaration.argument!);
    if (used.has(id)) throw new LanguageError(`Duplicate profile ${id}.`, declaration.argument!.range);
    used.add(id);
    if (declaration.profile) {
      throw new LanguageError('Profiles contain network settings, not another profile selection.', declaration.profile.range);
    }

    const config = {};
    writer.apply(declaration.properties, linkPatchFields, config, `fixed.network.profiles[${index}].config`);
    return { id, config };
  });
}
