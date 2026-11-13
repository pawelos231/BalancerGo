import type { Scenario } from '../scenarios/types.ts';
import { balancerFields, executionFields, linkFields, streamFields, tcpFields } from './bindings.ts';
import { LANGUAGE_VERSION, scenarioDefaults } from './defaults.ts';
import { FieldWriter } from './fields.ts';
import { backendAddresses, endpointName, prefixName } from './identity.ts';
import { parse } from './parser.ts';
import { compileProfiles } from './network.ts';
import { compileBackend, expandBackends } from './resources.ts';
import { LanguageError } from './source.ts';
import type { Declaration, Program, Property } from './syntax.ts';
import { countValue, timeValue } from './values.ts';
import { compileClientGroup, compileClientOverrides } from './workload.ts';
import { TimelineBuilder } from './timeline.ts';
import { applyTimelineCommands } from './timelineCommands.ts';

// Compile once before Start; the engine and Go CLI receive the unchanged JSON v2 contract.
export function compileScenario(source: string): Scenario {
  const program = parse(source);
  checkSupported(program);
  const run = oneDeclaration(program, 'run');
  const balancer = oneDeclaration(program, 'balancer');
  checkLanguageVersion(program);

  const scenario = scenarioDefaults(timeValue(run.argument!, 1n));
  const writer = new FieldWriter();
  writer.locations.set('fixed.execution', run.range);
  writer.locations.set('fixed.execution.durationNanoseconds', run.argument!.range);
  writer.apply(run.properties, executionFields, scenario.fixed.execution, 'fixed.execution');

  const defaults = optionalDeclaration(program, 'defaults');
  if (defaults) {
    const network: Property[] = [];
    const stream: Property[] = [];
    for (const property of defaults.properties) {
      const isNetwork = Object.prototype.hasOwnProperty.call(linkFields, property.key.text);
      (isNetwork ? network : stream).push(property);
    }
    writer.apply(network, linkFields, scenario.fixed.network.defaults, 'fixed.network.defaults');
    writer.apply(stream, streamFields, scenario.fixed.workload.defaults, 'fixed.workload.defaults');
  }
  scenario.fixed.network.profiles = compileProfiles(
    program.declarations.filter(declaration => declaration.keyword.text === 'profile'), writer,
  );
  const tcp = optionalDeclaration(program, 'tcp');
  if (tcp) {
    writer.locations.set('fixed.tcp', tcp.range);
    writer.apply(tcp.properties, tcpFields, scenario.fixed.tcp, 'fixed.tcp');
  }
  const fastRetransmitWasSet = writer.hasWritten('fixed.tcp.congestionControl.fastRetransmit');
  if (scenario.fixed.tcp.congestionControl?.mode === 'none' && !fastRetransmitWasSet) {
    scenario.fixed.tcp.congestionControl.fastRetransmit = false;
  }

  scenario.fixed.loadBalancer.id = prefixName(balancer.argument!);
  writer.locations.set('fixed.loadBalancer.id', balancer.argument!.range);
  writer.locations.set('states["0"].resources.loadBalancer', balancer.range);
  const state = scenario.states['0'];
  const lb = state.resources.loadBalancer;
  // Global endpoint defaults supply initial LB rates, while its physical ingress stays separate.
  lb.ingress.bandwidthBitsPerSecond = scenario.fixed.network.defaults.bandwidthBitsPerSecond;
  lb.forwardingBitsPerSecond = scenario.fixed.network.defaults.bandwidthBitsPerSecond;
  if (balancer.profile) {
    throw new LanguageError('The balancer cannot select an endpoint profile.', balancer.profile.range);
  }
  writer.apply(balancer.properties, balancerFields, lb, 'states["0"].resources.loadBalancer');

  const declarations = expandBackends(program.declarations.filter(item => {
    return item.keyword.text === 'backend' || item.keyword.text === 'backends';
  }));
  const ids = declarations.map(item => endpointName(item.argument!));
  const used = new Set<string>();
  for (const [index, id] of ids.entries()) {
    if (used.has(id)) throw new LanguageError(`Duplicate backend ${id}.`, declarations[index].argument!.range);
    used.add(id);
  }
  const addresses = backendAddresses(ids);
  state.resources.backends = declarations.map((declaration, index) => {
    return compileBackend(
      declaration, addresses.get(ids[index])!, writer, `states["0"].resources.backends[${index}]`,
    );
  });
  const groups = program.declarations.filter(item => item.keyword.text === 'clients');
  state.workload.clientGroups = groups.map((declaration, index) => compileClientGroup(
    declaration, writer, `states["0"].workload.clientGroups[${index}]`,
  ));
  compileClientOverrides(
    program.declarations.filter(declaration => declaration.keyword.text === 'client'),
    state.workload.clientGroups, writer, 'states["0"].workload',
  );

  // A backend called regular-1 cannot also denote the second client in group regular.
  const timeline = new TimelineBuilder(scenario, writer.locations, program.range);
  for (const block of program.timeline) {
    timeline.append(block, (draft, changes, path) => applyTimelineCommands(block.commands, draft, changes, path));
  }
  const slots = new Map<string, bigint>();
  for (const snapshot of Object.values(scenario.states)) {
    for (const group of snapshot.workload.clientGroups) {
      const previous = slots.get(group.id) ?? 0n;
      if (BigInt(group.count) > previous) slots.set(group.id, BigInt(group.count));
    }
  }
  for (const [index, id] of ids.entries()) {
    const dash = id.lastIndexOf('-');
    const count = slots.get(id.slice(0, dash)) ?? 0n;
    if (BigInt(id.slice(dash + 1)) < count) {
      throw new LanguageError(`Backend ${id} collides with a client alias.`, declarations[index].argument!.range);
    }
  }

  return timeline.finish();
}

// Reject recognized future syntax explicitly instead of silently dropping it from the JSON.
function checkSupported(program: Program): void {
  const supported = [
    'language', 'run', 'defaults', 'tcp', 'balancer', 'backend', 'backends', 'clients', 'client', 'profile',
  ];
  for (const declaration of program.declarations) {
    if (!supported.includes(declaration.keyword.text)) {
      throw new LanguageError(
        `${declaration.keyword.text} compilation is not available yet.`, declaration.keyword.range,
      );
    }
  }
}

function optionalDeclaration(program: Program, keyword: string): Declaration | undefined {
  const declarations = program.declarations.filter(item => item.keyword.text === keyword);
  if (declarations.length > 1) {
    throw new LanguageError(`Only one ${keyword} declaration is allowed.`, declarations[1].keyword.range);
  }
  return declarations[0];
}

function oneDeclaration(program: Program, keyword: string): Declaration {
  const declaration = optionalDeclaration(program, keyword);
  if (!declaration) throw new LanguageError(`Exactly one ${keyword} declaration is required.`, program.range);
  return declaration;
}

function checkLanguageVersion(program: Program): void {
  const language = optionalDeclaration(program, 'language');
  if (!language) return;
  if (program.declarations[0] !== language || language.properties.length > 0) {
    throw new LanguageError('language must be the first declaration with only its version.', language.range);
  }
  countValue(language.argument!, BigInt(LANGUAGE_VERSION), BigInt(LANGUAGE_VERSION));
}
