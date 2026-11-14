import type { ScenarioState } from '../scenarios/types.ts';
import { backendFields, balancerFields, clientStreamFields, mutableClientFields } from './bindings.ts';
import { FieldWriter } from './fields.ts';
import { clientAliasName, clientGroupName, endpointName } from './identity.ts';
import { writeEndpointNetwork } from './network.ts';
import { LanguageError } from './source.ts';
import type { Command } from './syntax.ts';
import { exactInteger } from './values.ts';
import { compileBackend } from './resources.ts';
import type { BackendPlan } from './resources.ts';
import { compileClientGroup } from './workload.ts';

// Apply source commands to a private copy; the builder publishes one complete state afterward.
export function applyTimelineCommands(
  commands: Command[],
  state: ScenarioState,
  writer: FieldWriter,
  path: string,
  context: { backends: BackendPlan; at: bigint },
): void {
  const backendIndices = new Map(state.resources.backends.map((backend, index) => [backend.id, index]));
  const groupIndices = new Map(state.workload.clientGroups.map((group, index) => [group.id, index]));
  const overrideIndices = new Map<string, Map<bigint, number>>();
  const removedGroups = new Set<string>();

  for (const command of commands) {
    if (command.kind === 'add') {
      if (command.declaration.keyword.text === 'clients') {
        const id = clientGroupName(command.declaration.argument!);
        if (groupIndices.has(id)) {
          throw new LanguageError(`Duplicate client group ${id}.`, command.declaration.argument!.range);
        }
        if (removedGroups.has(id)) {
          throw new LanguageError(`Client group ${id} cannot be reused after removal.`, command.declaration.argument!.range);
        }
        const index = state.workload.clientGroups.length;
        const group = compileClientGroup(
          command.declaration, new FieldWriter(writer.locations),
          `${path}.workload.clientGroups[${index}]`, exactInteger(context.at),
        );
        state.workload.clientGroups.push(group);
        groupIndices.set(id, index);
        continue;
      }
      const declarations = context.backends.additions.get(command);
      if (!declarations) {
        throw new LanguageError(`${command.declaration.keyword.text} additions are not available yet.`, command.range);
      }
      for (const declaration of declarations) {
        const id = endpointName(declaration.argument!);
        const index = state.resources.backends.length;
        const backend = compileBackend(
          declaration, context.backends.addresses.get(id)!, new FieldWriter(writer.locations),
          `${path}.resources.backends[${index}]`,
        );
        state.resources.backends.push(backend);
        backendIndices.set(id, index);
      }
      continue;
    }
    if (command.kind === 'down' || command.kind === 'up' || command.kind === 'drain') {
      const id = endpointName(command.name);
      const index = backendIndices.get(id);
      if (index === undefined) throw new LanguageError(`Unknown backend ${id}.`, command.name.range);
      const administrative = command.kind === 'drain';
      const field = administrative ? 'admin' : 'available';
      const value = administrative ? 'drain' : command.kind === 'up' ? 'true' : 'false';
      // Actions share the same field writer as set, so overlapping writes are still errors.
      writer.apply([{
        key: { kind: 'word', text: field, range: command.range },
        value: { kind: 'word', text: value, range: command.range },
      }], backendFields, state.resources.backends[index], `${path}.resources.backends[${index}]`);
      continue;
    }
    if (command.kind === 'remove') {
      const id = clientGroupName(command.name);
      if (!groupIndices.delete(id)) throw new LanguageError(`Unknown client group ${id}.`, command.name.range);
      removedGroups.add(id);
      continue;
    }
    if (command.kind === 'stop') {
      const id = clientGroupName(command.name);
      const index = groupIndices.get(id);
      if (index === undefined) throw new LanguageError(`Unknown client group ${id}.`, command.name.range);
      // Zero stops future births; the group and its reserved slots remain in every later state.
      writer.apply([{
        key: { kind: 'word', text: 'count', range: command.range },
        value: { kind: 'number', text: '0', range: command.range },
      }], mutableClientFields, state.workload.clientGroups[index], `${path}.workload.clientGroups[${index}]`);
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
    const name = individual ? clientAliasName(command.name!) : clientGroupName(command.name!);
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

  if (removedGroups.size > 0) removeClientGroups(state, removedGroups, writer, path);
}

// Keep indices stable while commands write conflict paths, then compact settings and origins together.
function removeClientGroups(
  state: ScenarioState,
  removed: ReadonlySet<string>,
  writer: FieldWriter,
  path: string,
): void {
  const retainedIndices = new Map<number, number>();
  state.workload.clientGroups = state.workload.clientGroups.filter((group, index) => {
    if (removed.has(group.id)) return false;
    retainedIndices.set(index, retainedIndices.size);
    return true;
  });

  const prefix = `${path}.workload.clientGroups[`;
  const origins = [...writer.locations].filter(([location]) => location.startsWith(prefix));
  for (const [location] of origins) writer.locations.delete(location);
  for (const [location, origin] of origins) {
    const end = location.indexOf(']', prefix.length);
    const previousIndex = Number(location.slice(prefix.length, end));
    const nextIndex = retainedIndices.get(previousIndex);
    if (nextIndex !== undefined) {
      writer.locations.set(`${prefix}${nextIndex}]${location.slice(end + 1)}`, origin);
    }
  }
}
