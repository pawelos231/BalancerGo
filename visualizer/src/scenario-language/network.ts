import type { ScenarioEndpointNetwork } from '../scenarios/types.ts';
import type { FieldWriter } from './fields.ts';
import type { Property } from './syntax.ts';
import { bitRate } from './values.ts';

// A shared endpoint link rate writes both directed patches; return other resource settings.
export function writeEndpointNetwork(
  properties: Property[],
  network: ScenarioEndpointNetwork,
  writer: FieldWriter,
  path: string,
): Property[] {
  const shared = properties.filter(property => property.key.text === 'link');
  const rate = { link: { path: 'bandwidthBitsPerSecond', read: bitRate } };
  writer.apply(shared, rate, network.toLoadBalancer, `${path}.toLoadBalancer`);
  writer.apply(shared, rate, network.fromLoadBalancer, `${path}.fromLoadBalancer`);
  return properties.filter(property => property.key.text !== 'link');
}
