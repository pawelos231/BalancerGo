import type { ScenarioState } from '../scenarios/types.ts';
import { backendFields, balancerFields, clientStreamFields, mutableClientFields } from './bindings.ts';
import { FieldWriter } from './fields.ts';
import { endpointName, prefixName } from './identity.ts';
import { writeEndpointNetwork } from './network.ts';
import { LanguageError } from './source.ts';
import type { Command } from './syntax.ts';
import { exactInteger } from './values.ts';
import { compileBackend } from './resources.ts';
import type { BackendPlan } from './resources.ts';

// Apply source commands to a private copy; the builder publishes one complete state afterward.
export function applyTimelineCommands(
  commands: Command[],
  state: ScenarioState,
  writer: FieldWriter,
  path: string,
  backendPlan: BackendPlan,
): void {
  const backendIndices = new Map(state.resources.backends.map((backend, index) => [backend.id, index]));
  const groupIndices = new Map(state.workload.clientGroups.map((group, index) => [group.id, index]));
  const overrideIndices = new Map<string, Map<bigint, number>>();

  for (const command of commands) {
    if (command.kind === 'add') {
      const declarations = backendPlan.additions.get(command);
      if (!declarations) {
        throw new LanguageError(`${command.declaration.keyword.text} additions are not available yet.`, command.range);
      }
      for (const declaration of declarations) {
        const id = endpointName(declaration.argument!);
        const index = state.resources.backends.length;
        const backend = compileBackend(
          declaration, backendPlan.addresses.get(id)!, new FieldWriter(writer.locations),
          `${path}.resources.backends[${index}]`,
        );
        state.resources.backends.push(backend);
        backendIndices.set(id, index);
      }
      continue;
    }
    if (command.kind !== 'set') {
      throw new LanguageError(`${command.kind} compilation is not available yet.`, command.range);
    }
    if (command.properties.length === 0) {
      throw new LanguageError('Set requires at least one setting.', command.range);
    }
    if (command.target.text === 'balancer') {
      writer.apply(command.properties, balancerFields, state.resources.loadBalancer, `${path}.resources.loadBalancer`);
      continue;
    }

    if (command.target.text === 'backend') {
      const id = endpointName(command.name!);
      const index = backendIndices.get(id);
      if (index === undefined) throw new LanguageError(`Unknown backend ${id}.`, command.name!.range);
      const backend = state.resources.backends[index];
      const backendPath = `${path}.resources.backends[${index}]`;
      const other = writeEndpointNetwork(command.properties, backend.network, writer, `${backendPath}.network`, {
        allowDirectionalOverrides: false,
      });
      writer.apply(other, backendFields, backend, backendPath);
      continue;
    }

    const individual = command.target.text === 'client';
    const name = individual ? endpointName(command.name!) : prefixName(command.name!);
    const dash = name.lastIndexOf('-');
    const groupName = individual ? name.slice(0, dash) : name;
    const groupIndex = groupIndices.get(groupName);
    if (groupIndex === undefined) throw new LanguageError(`Unknown client group ${groupName}.`, command.name!.range);
    const group = state.workload.clientGroups[groupIndex];
    const groupPath = `${path}.workload.clientGroups[${groupIndex}]`;

    if (!individual) {
      const other = writeEndpointNetwork(command.properties, group.network, writer, `${groupPath}.network`, {
        allowDirectionalOverrides: false,
      });
      writer.apply(other, mutableClientFields, group, groupPath);
      continue;
    }

    // Final native validation checks historical reservations, even after a smaller count.
    const index = BigInt(name.slice(dash + 1));
    if (index >= 16_777_215n) {
      throw new LanguageError('Client index exceeds the reserved slot limit.', command.name!.range);
    }
    const overrides = group.overrides!;
    let indices = overrideIndices.get(groupName);
    if (!indices) {
      indices = new Map(overrides.map((override, position) => [BigInt(override.index), position]));
      overrideIndices.set(groupName, indices);
    }
    let overrideIndex = indices.get(index);
    if (overrideIndex === undefined) {
      overrideIndex = overrides.length;
      overrides.push({
        index: exactInteger(index),
        network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} }, stream: {},
      });
      indices.set(index, overrideIndex);
    }
    const override = overrides[overrideIndex];
    const overridePath = `${groupPath}.overrides[${overrideIndex}]`;
    const other = writeEndpointNetwork(command.properties, override.network, writer, `${overridePath}.network`, {
      allowDirectionalOverrides: false,
    });
    writer.apply(other, clientStreamFields, override, overridePath);
    writer.locations.set(`${overridePath}.index`, command.name!.range);
  }
}
