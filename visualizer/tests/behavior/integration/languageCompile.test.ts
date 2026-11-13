import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { compileScenario } from '../../../src/scenario-language/compile.ts';
import { parseScenario } from '../../../src/scenarios/validation.ts';
import { resolveNetwork } from '../../../src/scenarios/settings.ts';

const fixture = (name: string) => readFileSync(new URL(`../../../../examples/language/${name}`, import.meta.url), 'utf8');
const minimal = 'run 1s\nbalancer lb {}\nbackend api-0 {}';

// The documented source must produce the documented native JSON, including pinned TCP defaults.
test('language compiler emits the exact validated static specification fixture', () => {
  assert.deepEqual(compileScenario(fixture('basic.lb')), parseScenario(fixture('basic.json')));
});

// Declaration order does not change endpoint addresses, and every compilation owns its data.
test('language static compilation preserves exact controls and deterministic backend identity', () => {
  const source = 'language 1\nrun 1s seed -9007199254740993 max-events 18446744073709551615 speed 0\nbalancer lb {}\n';
  const first = compileScenario(`${source}backend api-1 {}\nbackend api-0 {}`);
  const second = compileScenario(`${source}backend api-0 {}\nbackend api-1 {}`);
  const addresses = (scenario: typeof first) => Object.fromEntries(
    scenario.states['0'].resources.backends.map(backend => [backend.id, backend.address]),
  );
  assert.deepEqual(addresses(first), addresses(second));
  assert.equal(first.fixed.execution.seed, '-9007199254740993');
  assert.equal(first.fixed.execution.maxEvents, '18446744073709551615');
  first.fixed.network.defaults.queueCapacity = 0;
  assert.equal(second.fixed.network.defaults.queueCapacity, 1024);
  assert.equal(second.states['0'].workload.clientGroups.length, 0);
});

// Recognized syntax cannot vanish silently while its later semantic step is unfinished.
test('language static compiler rejects invalid identities, duplicates and unavailable features', () => {
  assert.throws(() => compileScenario(`${minimal}\nbackend api-0 {}`), /Duplicate backend/);
  assert.throws(() => compileScenario(minimal.replace('api-0', 'api-01')), /endpoint name/);
  assert.throws(() => compileScenario(`${minimal}\nbalancer another {}`), /Only one balancer/);
  assert.throws(() => compileScenario('run 1s'), /one balancer/);
  assert.throws(() => compileScenario(`${minimal}\nclient regular-0 {}`), /not available yet/);
  assert.throws(() => compileScenario(`${minimal}\nat 1ms {}`), /Timeline compilation/);
  assert.throws(() => compileScenario(`language 2\n${minimal}`), /between 1 and 1/);
  assert.throws(() => compileScenario(minimal.replace('lb {}', 'lb { algo p2c }')), /Expected one of/);
});

// The first complete workload fixture can be exported and run by the existing simulator.
test('language compiler emits compact client generators and inherited stream settings', () => {
  assert.deepEqual(compileScenario(fixture('transfer.lb')), parseScenario(fixture('transfer.json')));
  const result = compileScenario(`${minimal}
defaults payload 1MB segment-gap 1ms
clients regular { count 50000 every 10ms start 20ms payload null }
`);
  const groups = result.states['0'].workload.clientGroups;
  assert.equal(groups.length, 1);
  assert.equal(groups[0].count, 50_000);
  assert.equal(groups[0].clientGapNanoseconds, 10_000_000);
  assert.equal(groups[0].startAtNanoseconds, 20_000_000);
  assert.equal(groups[0].stream.bytes, null);
  assert.equal(result.fixed.workload.defaults.bytes, 1_000_000);
  assert.equal(result.fixed.workload.defaults.packetGapNanoseconds, 1_000_000);
});

// Native ID/slot limits still apply; DSL client aliases add a collision guard of their own.
test('language client generators reject duplicate groups, excessive budgets and alias collisions', () => {
  assert.throws(() => compileScenario(`${minimal}\nclients regular {}\nclients regular {}`), /already used/);
  assert.throws(() => compileScenario(`${minimal}\nclients huge { count 16777216 }`), /between/);
  assert.throws(() => compileScenario(`${minimal}\nclients api { count 1 }`), /client alias/);
  const result = compileScenario(`${minimal}\nclients regular { count 0 payload 0B every 0ns }`);
  assert.equal(result.states['0'].workload.clientGroups[0].count, 0);
});

// These are bindings to today's TCP, including nested timers and optional mechanisms.
test('language compiler maps current TCP controls without changing their behavior', () => {
  const result = compileScenario(`${minimal}
tcp {
    congestion newreno
    mss 1460B
    receive-buffer 32000B
    sack true
    ecn true
    client-idle 120s
    backend-half-open-idle 10s
    backend-established-idle 0ns
    congestion-control {
        fast-retransmit true
        initial-window 8
        maximum-window 16
        ssthresh 64KB
        fast-convergence false
    }
    rto { mode adaptive backoff true initial 1s minimum 200ms maximum 60s }
    close { msl 1s fin-wait 2s fin-retries 3 }
    delayed-ack { enabled true segments 2 max-delay 40ms }
}`);
  const tcp = result.fixed.tcp;
  assert.equal(tcp.congestionControl?.mode, 'newReno');
  assert.equal(tcp.congestionControl?.maximumWindowSegments, 16);
  assert.equal(tcp.congestionControl?.initialSlowStartThresholdBytes, 64_000);
  assert.equal(tcp.receiveBufferBytes, 32_000);
  assert.equal(tcp.ECNenabled, true);
  assert.equal(tcp.close?.maxFINRetries, 3);
  assert.equal(tcp.delayedAck?.maxDelayNanoseconds, 40_000_000);
  assert.equal(tcp.clientIdleTimeoutNanoseconds, 120_000_000_000);
});

// Explicit invalid combinations must be rejected, never silently corrected by the compiler.
test('language TCP binding preserves dependency checks and explicit nullable settings', () => {
  const unlimited = compileScenario(`${minimal}\ntcp congestion none`);
  assert.equal(unlimited.fixed.tcp.congestionControl?.fastRetransmit, false);
  const incompatibleFastRetry = `${minimal}\ntcp {
      congestion none
      congestion-control { fast-retransmit true }
  }`;
  assert.throws(() => compileScenario(incompatibleFastRetry), /requires/);
  assert.throws(() => compileScenario(`${minimal}\ntcp congestion tahoe sack true`), /requires Reno/);
  assert.throws(() => compileScenario(`${minimal}\ntcp { rto { minimum 2s initial 1s } }`), /minimum <= initial/);
  assert.throws(
    () => compileScenario(`${minimal}\ntcp { delayed-ack { max-delay 501ms } }`), /at most 500 ms/,
  );
  assert.throws(
    () => compileScenario(`${minimal}\ntcp { congestion reno congestion-control { mode cubic } }`), /overlaps/,
  );
  assert.equal(compileScenario(`${minimal}\ntcp receive-buffer null`).fixed.tcp.receiveBufferBytes, null);
});

// CPU queues and physical ingress/endpoint queues remain separate native resources.
test('language compiler binds CPU, ingress and directional network settings', () => {
  const result = compileScenario(`run 10s
defaults queue 50
balancer lb {
    ingress { link 100MB queue 32 travel 2us queue-policy threshold ecn-threshold 80% signal-probability 25% }
    host { cores 2 packet-time 10us queue 64 }
    mapping-timeouts { established 20s }
}
backend api-0 {
    link 1MB
    travel 2ms
    queue 16
    loss 1%
    to-lb { link 10MB travel 0ns queue 0 loss 0% }
    host { cores 4 packet-time 5us queue 128 }
}
clients regular { from-lb { link 2MB } link 5MB }
`);
  const state = result.states['0'];
  assert.equal(state.resources.loadBalancer.ingress.bandwidthBitsPerSecond, 800_000_000);
  assert.equal(state.resources.loadBalancer.ingress.congestionSignalProbability, 0.25);
  assert.equal(state.resources.loadBalancer.host?.packetProcessingTimeNanoseconds, 10_000);
  assert.equal(state.resources.loadBalancer.mappingTimeouts.establishedIdleTimeoutNanoseconds, 20_000_000_000);
  const backend = state.resources.backends[0];
  assert.equal(backend.network.toLoadBalancer.bandwidthBitsPerSecond, 80_000_000);
  assert.equal(backend.network.toLoadBalancer.queueCapacity, 0);
  assert.equal(backend.network.fromLoadBalancer.bandwidthBitsPerSecond, 8_000_000);
  assert.equal(backend.host?.queueCapacity, 128);
  assert.equal(state.workload.clientGroups[0].network.fromLoadBalancer.bandwidthBitsPerSecond, 16_000_000);
});

// Concrete CPU/ingress values cannot bypass their native limits or become client host settings.
test('language network and CPU binding rejects invalid settings and keeps null patches', () => {
  assert.throws(() => compileScenario(minimal.replace('api-0 {}', 'api-0 { host { cores 0 } }')), /between/);
  assert.throws(() => compileScenario(`${minimal}\nclients regular { host { cores 2 } }`), /Unknown setting/);
  assert.throws(() => compileScenario(minimal.replace('lb {}', 'lb { ingress { loss 101% } }')), /percentage/);
  const scenario = compileScenario(minimal.replace('api-0 {}', 'api-0 { travel null host { packet-time 0ns } }'));
  assert.equal(scenario.states['0'].resources.backends[0].network.toLoadBalancer.packetTravelTimeNanoseconds, null);
  assert.equal(scenario.states['0'].resources.backends[0].host?.packetProcessingTimeNanoseconds, 0);
});

// Resolve native patches to prove defaults -> profile -> shared endpoint -> direction precedence.
test('language compiler supports forward profiles and existing native inheritance', () => {
  const source = `run 1s
balancer lb {}
backend api-0 use mobile { link 20MB from-lb { link 30MB } }
clients regular use mobile { to-lb { travel 0ns queue 0 } }
profile mobile { link 10MB travel 20ms queue 7 loss 1% }
`;
  const scenario = compileScenario(source);
  const base = { toLoadBalancer: scenario.fixed.network.defaults, fromLoadBalancer: scenario.fixed.network.defaults };
  const profiles = new Map(scenario.fixed.network.profiles.map(profile => [profile.id, profile.config]));
  const backend = resolveNetwork(base, scenario.states['0'].resources.backends[0].network, profiles, 'test');
  const clients = resolveNetwork(base, scenario.states['0'].workload.clientGroups[0].network, profiles, 'test');
  assert.equal(backend.toLoadBalancer.bandwidthBitsPerSecond, 160_000_000);
  assert.equal(backend.fromLoadBalancer.bandwidthBitsPerSecond, 240_000_000);
  assert.equal(clients.toLoadBalancer.packetTravelTimeNanoseconds, 0);
  assert.equal(clients.toLoadBalancer.queueCapacity, 0);
  assert.equal(clients.fromLoadBalancer.packetTravelTimeNanoseconds, 20_000_000);
  assert.equal(clients.fromLoadBalancer.lossRate, 0.01);

  // Inline settings must produce the same physical links as the selected profile.
  const inlineSource = minimal.replace('api-0 {}', 'api-0 { link 10MB travel 20ms queue 7 loss 1% }');
  const inline = compileScenario(inlineSource);
  const inlineLinks = resolveNetwork(base, inline.states['0'].resources.backends[0].network, new Map(), 'test');
  const profileOnly = compileScenario(source.replace('link 20MB from-lb { link 30MB }', ''));
  const profileLinks = resolveNetwork(base, profileOnly.states['0'].resources.backends[0].network, profiles, 'test');
  assert.deepEqual(profileLinks, inlineLinks);
});

// Unused definitions are checked too; missing names and duplicate selections never disappear.
test('language profiles reject invalid definitions, missing references and duplicate selections', () => {
  assert.throws(() => compileScenario(`${minimal}\nprofile mobile {}\nprofile mobile {}`), /Duplicate profile/);
  assert.throws(() => compileScenario(minimal.replace('api-0 {}', 'api-0 use missing {}')), /unknown profile/);
  assert.throws(() => compileScenario(`${minimal}\nprofile invalid { queue -1 }`), /between/);
  assert.throws(
    () => compileScenario(`${minimal.replace('api-0 {}', 'api-0 use mobile { use mobile }')}\nprofile mobile {}`),
    /overlaps/,
  );
});

// A template is syntax sugar: explicit backends have the same IDs, addresses and settings.
test('language bulk backends expand to equivalent independent native resources', () => {
  const base = 'run 1s\nbalancer lb {}\n';
  const bulk = compileScenario(`${base}backends api { count 3 from 2 link 10MB host { cores 2 } }`);
  const explicit = compileScenario(`${base}${[2, 3, 4].map(index => {
    return `backend api-${index} { link 10MB host { cores 2 } }`;
  }).join('\n')}`);
  assert.deepEqual(bulk, explicit);
  bulk.states['0'].resources.backends[0].network.toLoadBalancer.queueCapacity = 0;
  assert.equal(bulk.states['0'].resources.backends[1].network.toLoadBalancer.queueCapacity, undefined);

  const largeSuffix = compileScenario(`${base}backends api { count 2 from 9007199254740993 }`);
  assert.deepEqual(largeSuffix.states['0'].resources.backends.map(backend => backend.id), [
    'api-9007199254740993', 'api-9007199254740994',
  ]);
});

// Check count before expansion, and check generated IDs against explicit declarations and aliases.
test('language bulk backends reject excessive counts and overlapping resource identities', () => {
  assert.throws(() => compileScenario(`${minimal}\nbackends api { count 1 }`), /Duplicate backend/);
  assert.throws(() => compileScenario(`${minimal}\nbackends extra { count 10001 }`), /between/);
  assert.throws(() => compileScenario(`${minimal}\nbackends extra {}`), /require a count/);
  const collision = 'run 1s\nbalancer lb {}\nbackends api { count 1 }\nclients api {}';
  assert.throws(() => compileScenario(collision), /client alias/);
  assert.throws(() => compileScenario('run 1s\nbalancer lb {}\nbackends api { count 0 }'), /between/);
});
