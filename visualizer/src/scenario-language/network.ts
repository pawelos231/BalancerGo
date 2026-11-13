import type { ScenarioEndpointNetwork } from '../scenarios/types.ts';
import { FieldWriter } from './fields.ts';
import { linkFields, linkPatchFields } from './bindings.ts';
import type { Property } from './syntax.ts';

// Apply shared endpoint settings first, then directional overrides, regardless of source order.
export function writeEndpointNetwork(
  properties: Property[],
  network: ScenarioEndpointNetwork,
  writer: FieldWriter,
  path: string,
): Property[] {
  const shared: Property[] = [];
  const directions: Property[] = [];
  const other: Property[] = [];
  for (const property of properties) {
    const name = property.key.text;
    if (Object.prototype.hasOwnProperty.call(linkFields, name)) shared.push(property);
    else if (name === 'to-lb' || name === 'from-lb') directions.push(property);
    else other.push(property);
  }

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
