import type { Scenario, ScenarioBackendState } from '../scenarios/types.ts';
import { serializeScenario } from '../scenarios/files.ts';
import { validateScenario } from '../scenarios/validation.ts';
import { balancerFields, executionFields, linkFields, tcpFields } from './bindings.ts';
import { defaultHost, LANGUAGE_VERSION, scenarioDefaults } from './defaults.ts';
import { FieldWriter } from './fields.ts';
import { backendAddresses, endpointName, prefixName } from './identity.ts';
import { parse } from './parser.ts';
import { LanguageError } from './source.ts';
import type { Declaration, Program } from './syntax.ts';
import { bitRate, countValue, textValue, timeValue } from './values.ts';

// Compile once before Start; the engine and Go CLI receive the unchanged JSON v2 contract.
export function compileScenario(source: string): Scenario {
  const program = parse(source);
  checkSupported(program);
  const run = oneDeclaration(program, 'run');
  const balancer = oneDeclaration(program, 'balancer');
  checkLanguageVersion(program);

  const scenario = scenarioDefaults(timeValue(run.argument!, 1n));
  const writer = new FieldWriter();
  writer.apply(run.properties, executionFields, scenario.fixed.execution, 'fixed.execution');

  const defaults = optionalDeclaration(program, 'defaults');
  if (defaults) {
    writer.apply(defaults.properties, linkFields, scenario.fixed.network.defaults, 'fixed.network.defaults');
  }
  const tcp = optionalDeclaration(program, 'tcp');
  if (tcp) writer.apply(tcp.properties, tcpFields, scenario.fixed.tcp, 'fixed.tcp');
  if (scenario.fixed.tcp.congestionControl?.mode === 'none') {
    scenario.fixed.tcp.congestionControl.fastRetransmit = false;
  }

  scenario.fixed.loadBalancer.id = prefixName(balancer.argument!);
  const state = scenario.states['0'];
  const lb = state.resources.loadBalancer;
  // Global endpoint defaults supply initial LB rates, while its physical ingress stays separate.
  lb.ingress.bandwidthBitsPerSecond = scenario.fixed.network.defaults.bandwidthBitsPerSecond;
  lb.forwardingBitsPerSecond = scenario.fixed.network.defaults.bandwidthBitsPerSecond;
  if (balancer.profile) {
    throw new LanguageError('The balancer cannot select an endpoint profile.', balancer.profile.range);
  }
  writer.apply(balancer.properties, balancerFields, lb, 'states["0"].resources.loadBalancer');

  const declarations = program.declarations.filter(item => item.keyword.text === 'backend');
  const ids = declarations.map(item => endpointName(item.argument!));
  const used = new Set<string>();
  for (const [index, id] of ids.entries()) {
    if (used.has(id)) throw new LanguageError(`Duplicate backend ${id}.`, declarations[index].argument!.range);
    used.add(id);
  }
  if (ids.length > 10_000) throw new LanguageError('At most 10,000 backends are supported.', program.range);
  const addresses = backendAddresses(ids);
  state.resources.backends = declarations.map((declaration, index) => {
    return compileBackend(declaration, addresses.get(ids[index])!, writer, index);
  });

  try {
    const checked = validateScenario(scenario);
    serializeScenario(checked);
    return checked;
  } catch (error) {
    const message = error instanceof Error ? error.message : 'Invalid compiled scenario.';
    throw new LanguageError(message, program.range);
  }
}

function compileBackend(
  declaration: Declaration,
  address: string,
  writer: FieldWriter,
  index: number,
): ScenarioBackendState {
  if (declaration.profile) {
    throw new LanguageError('Profile compilation is not available yet.', declaration.profile.range);
  }
  const backend: ScenarioBackendState = {
    id: endpointName(declaration.argument!), address, port: 80,
    host: defaultHost(),
    network: { profile: '', toLoadBalancer: {}, fromLoadBalancer: {} },
  };
  const path = `states["0"].resources.backends[${index}]`;
  const shared = declaration.properties.filter(property => property.key.text === 'link');
  const other = declaration.properties.filter(property => property.key.text !== 'link');
  const rate = { link: { path: 'bandwidthBitsPerSecond', read: bitRate } };
  writer.apply(shared, rate, backend.network.toLoadBalancer, `${path}.network.toLoadBalancer`);
  writer.apply(shared, rate, backend.network.fromLoadBalancer, `${path}.network.fromLoadBalancer`);
  writer.apply(other, { name: { path: 'name', read: textValue } }, backend, path);
  return backend;
}

// Reject recognized future syntax explicitly instead of silently dropping it from the JSON.
function checkSupported(program: Program): void {
  const supported = ['language', 'run', 'defaults', 'tcp', 'balancer', 'backend'];
  for (const declaration of program.declarations) {
    if (!supported.includes(declaration.keyword.text)) {
      throw new LanguageError(
        `${declaration.keyword.text} compilation is not available yet.`, declaration.keyword.range,
      );
    }
  }
  if (program.timeline.length > 0) {
    throw new LanguageError('Timeline compilation is not available yet.', program.timeline[0].range);
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
