import type { ExactInteger, ScenarioClientGroup } from '../scenarios/types.ts';
import { clientFields } from './bindings.ts';
import type { FieldWriter } from './fields.ts';
import { prefixName } from './identity.ts';
import { writeEndpointNetwork } from './network.ts';
import { LanguageError } from './source.ts';
import type { Declaration } from './syntax.ts';

// Keep a compact generator: count 50,000 never creates 50,000 compiler-side clients.
export function compileClientGroup(
  declaration: Declaration,
  writer: FieldWriter,
  path: string,
  startAt: ExactInteger = 0,
): ScenarioClientGroup {
  if (declaration.profile) {
    throw new LanguageError('Profile compilation is not available yet.', declaration.profile.range);
  }
  const group: ScenarioClientGroup = {
    id: prefixName(declaration.argument!), count: 1,
    startAtNanoseconds: startAt, clientGapNanoseconds: 0,
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
    stream: {}, overrides: [],
  };
  const other = writeEndpointNetwork(declaration.properties, group.network, writer, `${path}.network`);
  writer.apply(other, clientFields, group, path);
  return group;
}
