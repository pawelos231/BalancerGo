import assert from 'node:assert/strict';
import { isIPv4 } from 'node:net';
import test from 'node:test';
import { scenarioFormValues, validateScenarioField } from '../src/scenarios/draft.ts';
import type { ScenarioField } from '../src/scenarios/draft.ts';
import { serializeScenario } from '../src/scenarios/files.ts';
import { presetCategories, scenarioPresets } from '../src/scenarios/presets.ts';
import type { ScenarioEndpointNetwork } from '../src/scenarios/types.ts';
import { scenarioFixture } from './scenarioFixture.ts';

const originalNetworkIds = [
  'mixed-clients',
  'mixed-backends',
  'ingress-bottleneck',
  'egress-bottleneck',
];

function buildPreset(id: string) {
  const preset = scenarioPresets.find((entry) => entry.id === id);
  assert.ok(preset, `missing preset ${id}`);
  return preset.build(scenarioFixture());
}

test('preset factories keep backend defaults exact and return independent documents', () => {
  const defaults = scenarioFixture();
  const original = structuredClone(defaults);
  assert.equal(new Set(scenarioPresets.map((preset) => preset.id)).size, scenarioPresets.length);
  const networkPresets = scenarioPresets.filter((preset) => originalNetworkIds.includes(preset.id));
  assert.equal(networkPresets.length, 4);

  for (const preset of networkPresets) {
    const result = preset.build(defaults);
    assert.equal(result.execution.seed, '-9007199254740993');
    assert.equal(result.execution.maxEvents, '18446744073709551615');
    assert.equal(result.execution.snapshotIntervalNanoseconds, 12_345_678);
    assert.equal(result.execution.durationNanoseconds, 180_000_000_000);
    assert.equal(result.execution.speed, 1);
    assert.equal(result.network.defaults.packetTravelTimeNanoseconds, 765_432);
    assert.equal(result.network.defaults.queueCapacity, 17);
    assert.equal(result.resources.loadBalancer.ingress.queueCapacity, 5);
    assert.equal(result.resources.loadBalancer.ingress.packetTravelTimeNanoseconds, 123);
    assert.equal(result.resources.loadBalancer.algorithm, 'random');
    assert.equal(result.workload.defaults.bytes, 1_073_741_824);
    assert.equal(result.workload.defaults.packetGapNanoseconds, 0);
    assert.equal(result.resources.backends.length, 2);
    assert.equal(new Set(result.resources.backends.map((backend) => backend.address)).size, 2);

    result.resources.loadBalancer.ingress.queueCapacity = 999;
    assert.deepEqual(defaults, original);
    assert.equal(preset.build(defaults).resources.loadBalancer.ingress.queueCapacity, 5);
  }
});

test('preset profiles and shared limits identify four different experiments', () => {
  const defaults = scenarioFixture();
  const mixed = scenarioPresets.find((preset) => preset.id === 'mixed-clients')!.build(defaults);
  assert.deepEqual(mixed.workload.clientGroups.map((group) => [group.id, group.count]), [
    ['slow-clients', 8], ['fast-clients', 8],
  ]);
  assert.equal(mixed.workload.clientGroups[0].network.profile, 'slow-client');
  assert.equal(mixed.workload.clientGroups[1].network.profile, undefined);
  assert.deepEqual(mixed.network.profiles, [{
    id: 'slow-client', config: { bandwidthBitsPerSecond: 10_000_000 },
  }]);

  const backends = scenarioPresets.find((preset) => preset.id === 'mixed-backends')!.build(defaults);
  assert.equal(backends.resources.backends[0].network.profile, 'slow-backend');
  assert.equal(backends.resources.backends[1].network.profile, undefined);

  const ingress = scenarioPresets.find((preset) => preset.id === 'ingress-bottleneck')!.build(defaults);
  const egress = scenarioPresets.find((preset) => preset.id === 'egress-bottleneck')!.build(defaults);
  for (const result of [ingress, egress]) {
    assert.equal(result.workload.clientGroups[0].count, 32);
    assert.equal(result.network.defaults.bandwidthBitsPerSecond, 1_000_000_000);
  }
  assert.equal(ingress.resources.loadBalancer.ingress.bandwidthBitsPerSecond, 10_000_000);
  assert.equal(ingress.resources.loadBalancer.forwardingBitsPerSecond, 1_000_000_000);
  assert.equal(egress.resources.loadBalancer.ingress.bandwidthBitsPerSecond, 1_000_000_000);
  assert.equal(egress.resources.loadBalancer.forwardingBitsPerSecond, 10_000_000);
});

test('throughput preset keeps demand bounded, reproducible and independent of network defaults', () => {
  const defaults = scenarioFixture();
  const before = structuredClone(defaults);
  const preset = scenarioPresets.find((entry) => entry.id === 'simulator-throughput')!;
  assert.equal(preset.title, 'Simulator throughput');
  const scenario = preset.build(defaults);
  assert.equal(scenario.execution.durationNanoseconds, 5_000_000);
  assert.equal(scenario.execution.snapshotIntervalNanoseconds, 50_000);
  assert.equal(scenario.execution.speed, 0);
  assert.equal(scenario.execution.seed, 1);
  assert.equal(scenario.execution.maxEvents, 50_000_000);
  assert.equal(scenario.resources.loadBalancer.algorithm, 'roundRobin');
  assert.deepEqual(scenario.workload.clientGroups.map((group) => [
    group.count, group.startAtNanoseconds, group.clientGapNanoseconds,
  ]), [[50, 0, 0]]);
  assert.equal(scenario.workload.defaults.bytes, 1_073_741_824);
  assert.equal(scenario.workload.defaults.packetGapNanoseconds, 0);

  const backendProfile = scenario.network.profiles!.find((profile) => profile.id === 'throughput-backend')!;
  assert.equal(scenario.resources.backends.length, 5);
  for (const backend of scenario.resources.backends) {
    assert.equal(backend.network.profile, backendProfile.id);
    assert.deepEqual(backend.network.toLoadBalancer, {});
    assert.deepEqual(backend.network.fromLoadBalancer, {});
  }
  assert.equal(new Set(scenario.resources.backends.map((backend) => backend.id)).size, 5);
  assert.equal(new Set(scenario.resources.backends.map((backend) => `${backend.address}:${backend.port}`)).size, 5);

  // Both directions inherit zero travel/loss and finite waiting slots.
  assert.equal(scenario.network.defaults.packetTravelTimeNanoseconds, 0);
  assert.equal(scenario.network.defaults.lossRate, 0);
  assert.equal(scenario.network.defaults.queueCapacity, 4096);
  assert.equal(scenario.resources.loadBalancer.ingress.packetTravelTimeNanoseconds, 0);
  assert.equal(scenario.resources.loadBalancer.ingress.lossRate, 0);
  assert.equal(scenario.resources.loadBalancer.ingress.queueCapacity, 4096);

  const rates = [
    scenario.network.defaults.bandwidthBitsPerSecond,
    backendProfile.config.bandwidthBitsPerSecond!,
    scenario.resources.loadBalancer.ingress.bandwidthBitsPerSecond,
    scenario.resources.loadBalancer.forwardingBitsPerSecond,
  ];
  assert.deepEqual(rates, [
    80_000_000_000_000,
    800_000_000_000_000,
    8_000_000_000_000_000,
    8_000_000_000_000_000,
  ]);
  for (const rate of rates) {
    assert.ok(Number.isSafeInteger(rate));
    // Every ACK and full segment reaches the numerical floor, rather than a link limit.
    for (const bytes of [40n, 1064n]) {
      const bitsNanoseconds = bytes * 8n * 1_000_000_000n;
      const nanoseconds = (bitsNanoseconds + BigInt(rate) - 1n) / BigInt(rate);
      assert.equal(nanoseconds, 1n);
    }
  }

  const fields = scenarioFormValues(scenario);
  for (const [name, raw] of Object.entries(fields)) {
    assert.equal(validateScenarioField(name as ScenarioField, raw), undefined, name);
  }
  assert.deepEqual(defaults, before);
  assert.deepEqual(preset.build(defaults), scenario);
  scenario.resources.backends[0].network.fromLoadBalancer.lossRate = 1;
  assert.deepEqual(preset.build(defaults).resources.backends[0].network.fromLoadBalancer, {});
});

test('limits benchmark configures independent endpoints and bounded zero-delay paths', () => {
  const scenario = buildPreset('simulator-limits');
  const backends = scenario.resources.backends;
  const [group] = scenario.workload.clientGroups;
  const backendProfile = scenario.network.profiles!.find((profile) => profile.id === 'limits-backend')!;

  assert.equal(scenarioPresets[0].title, 'Absolute simulator limits');
  assert.deepEqual(scenario.execution, {
    durationNanoseconds: 1_000_000,
    snapshotIntervalNanoseconds: 1_000_000,
    seed: 1,
    speed: 0,
    maxEvents: 20_000_000,
  });
  assert.equal(scenario.resources.loadBalancer.algorithm, 'roundRobin');
  assert.equal(scenario.workload.clientGroups.length, 1);
  assert.equal(group.count, 50_000);
  assert.equal(group.startAtNanoseconds, 0);
  assert.equal(group.clientGapNanoseconds, 0);
  assert.deepEqual(group.stream, {});
  assert.equal(scenario.workload.defaults.bytes, 1_073_741_824);
  assert.equal(scenario.workload.defaults.packetGapNanoseconds, 0);
  assert.equal(backends.length, 100);
  assert.equal(new Set(backends.map((backend) => backend.id)).size, 100);
  assert.equal(new Set(backends.map((backend) => backend.address)).size, 100);

  for (const backend of backends) {
    assert.ok(isIPv4(backend.address));
    assert.equal(backend.port, 80);
    assert.equal(backend.network.profile, backendProfile.id);
    assert.deepEqual(backend.network.toLoadBalancer, {});
    assert.deepEqual(backend.network.fromLoadBalancer, {});
  }

  // Resolve the five physical path types; the forwarding budget is checked separately.
  const defaults = scenario.network.defaults;
  const backendNetwork = backends[0].network;
  const pathConfigs = [
    { ...defaults, ...group.network.toLoadBalancer },
    { ...defaults, ...group.network.fromLoadBalancer },
    { ...defaults, ...backendProfile.config, ...backendNetwork.toLoadBalancer },
    { ...defaults, ...backendProfile.config, ...backendNetwork.fromLoadBalancer },
    scenario.resources.loadBalancer.ingress,
  ];
  assert.deepEqual(pathConfigs.map((config) => config.bandwidthBitsPerSecond), [
    80_000_000_000_000,
    80_000_000_000_000,
    800_000_000_000_000,
    800_000_000_000_000,
    '80000000000000000',
  ]);

  for (const [index, config] of pathConfigs.entries()) {
    assert.equal(config.packetTravelTimeNanoseconds, 0);
    assert.equal(config.lossRate, 0);
    assert.equal(config.queueCapacity, index === 4 ? 131_072 : 4096);
  }

  const rates = [
    ...pathConfigs.map((config) => config.bandwidthBitsPerSecond),
    scenario.resources.loadBalancer.forwardingBitsPerSecond,
  ];
  for (const rate of rates) {
    if (typeof rate === 'number') assert.ok(Number.isSafeInteger(rate));
    const exactRate = BigInt(rate);
    assert.ok(exactRate > 0n && exactRate <= 9_223_372_036_854_775_807n);

    for (const wireBytes of [40n, 1064n]) {
      const bitsNanoseconds = wireBytes * 8n * 1_000_000_000n;
      assert.equal((bitsNanoseconds + exactRate - 1n) / exactRate, 1n);
    }
  }
});

test('limits benchmark preserves unsafe integer rates through form projection and JSON export', () => {
  const scenario = buildPreset('simulator-limits');
  const fields = scenarioFormValues(scenario);
  const exactSharedRate = '80000000000000000';

  assert.equal(fields.forwardingBitsPerSecond, exactSharedRate);
  assert.equal(fields.ingressBandwidthBitsPerSecond, exactSharedRate);
  for (const [name, raw] of Object.entries(fields)) {
    assert.equal(validateScenarioField(name as ScenarioField, raw), undefined, name);
  }

  const document = serializeScenario(scenario);
  const roundTripped = JSON.parse(document) as typeof scenario;
  assert.equal(roundTripped.resources.loadBalancer.forwardingBitsPerSecond, exactSharedRate);
  assert.equal(roundTripped.resources.loadBalancer.ingress.bandwidthBitsPerSecond, exactSharedRate);
  assert.deepEqual(roundTripped, scenario);
});

test('catalog pins the limits benchmark and keeps unique categories and original relative order', () => {
  assert.equal(scenarioPresets.length, 18);
  assert.equal(scenarioPresets[0].id, 'simulator-limits');
  assert.equal(scenarioPresets[0].category, 'performance');
  assert.equal(new Set(scenarioPresets.map((preset) => preset.id)).size, scenarioPresets.length);
  const remainingPresets = scenarioPresets.filter((preset) => preset.id !== 'simulator-limits');
  assert.deepEqual(remainingPresets.slice(0, 5).map((preset) => preset.id), [
    ...originalNetworkIds,
    'simulator-throughput',
  ]);

  const categories = new Set(presetCategories.map((category) => category.id));
  assert.equal(categories.size, presetCategories.length);
  assert.deepEqual([...categories].sort(), [
    'edge-cases', 'links', 'loss', 'performance', 'queues', 'routing', 'workload',
  ]);

  for (const category of presetCategories) {
    assert.ok(category.label.trim(), `${category.id}: category label`);
    assert.ok(scenarioPresets.some((preset) => preset.category === category.id), category.id);
  }

  for (const preset of scenarioPresets) {
    assert.ok(categories.has(preset.category), `${preset.id}: category`);
    assert.ok(preset.title.trim(), `${preset.id}: title`);
    assert.ok(preset.description.trim(), `${preset.id}: description`);
  }
});

test('every builder is deterministic and isolates nested configuration from other builds', () => {
  const defaults = scenarioFixture();
  const originalDefaults = structuredClone(defaults);

  for (const preset of scenarioPresets) {
    const first = preset.build(defaults);
    const second = preset.build(defaults);
    const expected = structuredClone(first);

    assert.deepEqual(second, expected, `${preset.id}: deterministic build`);

    // Change separate nested branches to catch shared defaults or cached documents.
    first.execution.seed = 42;
    first.network.defaults.queueCapacity = 999;
    first.resources.loadBalancer.ingress.lossRate = 0.5;
    first.workload.defaults.bytes = 7;

    const profile = first.network.profiles?.[0];
    const backend = first.resources.backends[0];
    const group = first.workload.clientGroups[0];

    if (profile) profile.config.queueCapacity = 999;
    if (backend) backend.network.fromLoadBalancer.lossRate = 0.5;
    if (group) group.network.toLoadBalancer.queueCapacity = 999;
    if (group?.overrides?.[0]) group.overrides[0].stream.bytes = 7;

    assert.deepEqual(second, expected, `${preset.id}: independent prior build`);
    assert.deepEqual(preset.build(defaults), expected, `${preset.id}: independent next build`);
    assert.deepEqual(defaults, originalDefaults, `${preset.id}: backend defaults`);
  }
});

test('every preset has valid resource membership and resolvable profile references', () => {
  for (const preset of scenarioPresets) {
    const scenario = preset.build(scenarioFixture());
    const backends = scenario.resources.backends;
    const groups = scenario.workload.clientGroups;
    const resourceIds = [
      scenario.resources.loadBalancer.id,
      ...backends.map((backend) => backend.id),
      ...groups.map((group) => group.id),
    ];
    const profiles = scenario.network.profiles ?? [];
    const profileIds = new Set(profiles.map((profile) => profile.id));

    assert.equal(BigInt(scenario.version), 1n, `${preset.id}: scenario version`);
    assert.equal(new Set(resourceIds).size, resourceIds.length, `${preset.id}: resource IDs`);
    assert.ok(resourceIds.every((id) => id.trim() !== ''), `${preset.id}: nonempty resource IDs`);
    assert.equal(profileIds.size, profiles.length, `${preset.id}: profile IDs`);

    const endpoints = backends.map((backend) => `${backend.address}:${backend.port}`);
    assert.equal(new Set(endpoints).size, endpoints.length, `${preset.id}: backend endpoints`);
    for (const backend of backends) {
      const port = BigInt(backend.port);
      assert.ok(port > 0n && port <= 65535n, `${preset.id}: backend port`);
    }

    const networks: ScenarioEndpointNetwork[] = [
      ...backends.map((backend) => backend.network),
      ...groups.map((group) => group.network),
    ];
    let totalClients = 0n;

    for (const group of groups) {
      const count = BigInt(group.count);
      const overrides = group.overrides ?? [];

      assert.ok(count >= 0n, `${preset.id}: nonnegative group count`);
      assert.ok(BigInt(group.startAtNanoseconds) >= 0n, `${preset.id}: arrival time`);
      assert.ok(BigInt(group.clientGapNanoseconds) >= 0n, `${preset.id}: arrival gap`);
      const indices = overrides.map((override) => String(override.index));
      assert.equal(new Set(indices).size, overrides.length, `${preset.id}: unique override indices`);

      for (const override of overrides) {
        const index = BigInt(override.index);
        assert.ok(index >= 0n && index < count, `${preset.id}: override belongs to a client`);
        networks.push(override.network);
      }

      totalClients += count;
    }

    assert.ok(totalClients <= 16_777_215n, `${preset.id}: total client limit`);
    for (const network of networks) {
      if (network.profile) {
        assert.ok(profileIds.has(network.profile), `${preset.id}: profile ${network.profile}`);
      }
    }
  }
});

test('new network experiments reset unrelated loss, timing and execution defaults', () => {
  const inheritedOrPerformanceIds = new Set([
    ...originalNetworkIds,
    'simulator-throughput',
    'simulator-limits',
  ]);
  const experiments = scenarioPresets.filter((preset) => !inheritedOrPerformanceIds.has(preset.id));
  assert.equal(experiments.length, 12);

  for (const preset of experiments) {
    const scenario = preset.build(scenarioFixture());

    assert.equal(scenario.execution.durationNanoseconds, 180_000_000_000, preset.id);
    assert.equal(scenario.execution.speed, 1, preset.id);
    assert.equal(scenario.workload.defaults.bytes, 1_073_741_824, preset.id);
    assert.equal(scenario.network.defaults.lossRate, 0, preset.id);
    assert.equal(scenario.network.defaults.packetTravelTimeNanoseconds, 1_000_000, preset.id);
    assert.equal(scenario.network.defaults.queueCapacity, 1024, preset.id);

    if (preset.id !== 'seeded-random') {
      assert.equal(scenario.execution.seed, 1, preset.id);
      assert.equal(scenario.resources.loadBalancer.algorithm, 'roundRobin', preset.id);
    }
  }
});

test('directional client limits and travel profiles describe distinct access paths', () => {
  const asymmetric = buildPreset('asymmetric-clients');
  const [upload, download] = asymmetric.workload.clientGroups;
  const fastRate = asymmetric.network.defaults.bandwidthBitsPerSecond;

  assert.ok(BigInt(upload.network.toLoadBalancer.bandwidthBitsPerSecond!) < BigInt(fastRate));
  assert.deepEqual(upload.network.fromLoadBalancer, {});
  assert.deepEqual(download.network.toLoadBalancer, {});
  assert.equal(
    download.network.fromLoadBalancer.bandwidthBitsPerSecond,
    upload.network.toLoadBalancer.bandwidthBitsPerSecond,
  );

  const latency = buildPreset('mixed-latency');
  const clientProfiles = latency.workload.clientGroups.map((group) => (
    latency.network.profiles!.find((profile) => profile.id === group.network.profile)!
  ));
  assert.equal(new Set(clientProfiles.map((profile) => profile.id)).size, 2);
  const delays = clientProfiles.map((profile) => BigInt(profile.config.packetTravelTimeNanoseconds!));
  assert.ok(delays[0] < BigInt(latency.network.defaults.packetTravelTimeNanoseconds));
  assert.ok(delays[1] > BigInt(latency.network.defaults.packetTravelTimeNanoseconds));

  for (const group of latency.workload.clientGroups) {
    assert.deepEqual(group.network.toLoadBalancer, {});
    assert.deepEqual(group.network.fromLoadBalancer, {});
  }
});

test('slow ACK paths constrain backend returns rather than client data delivery', () => {
  const scenario = buildPreset('slow-ack-path');
  const dataRate = BigInt(scenario.network.defaults.bandwidthBitsPerSecond);

  for (const backend of scenario.resources.backends) {
    const returnRate = BigInt(backend.network.toLoadBalancer.bandwidthBitsPerSecond!);
    assert.ok(returnRate > 0n && returnRate < dataRate);
    assert.deepEqual(backend.network.fromLoadBalancer, {});
  }
  assert.equal(
    scenario.resources.loadBalancer.forwardingBitsPerSecond,
    scenario.network.defaults.bandwidthBitsPerSecond,
  );
});

test('probabilistic client loss and a single blackholed backend affect different branches', () => {
  const lossy = buildPreset('lossy-access');

  for (const group of lossy.workload.clientGroups) {
    const loss = group.network.toLoadBalancer.lossRate!;
    assert.ok(loss > 0 && loss < 1);
    assert.equal(group.network.fromLoadBalancer.lossRate ?? lossy.network.defaults.lossRate, 0);
  }

  const blackhole = buildPreset('blackholed-backend');
  const branchLosses = blackhole.resources.backends.map((backend) => (
    backend.network.fromLoadBalancer.lossRate ?? blackhole.network.defaults.lossRate
  ));
  assert.deepEqual(branchLosses.sort(), [0, 1]);
  assert.equal(blackhole.resources.loadBalancer.algorithm, 'roundRobin');
  for (const backend of blackhole.resources.backends) {
    assert.equal(backend.network.toLoadBalancer.lossRate ?? blackhole.network.defaults.lossRate, 0);
  }
});

test('finite and unbuffered ingress experiments preserve the same arrival burst', () => {
  const tiny = buildPreset('tiny-queues');
  const unbuffered = buildPreset('unbuffered-ingress');
  const tinyIngress = tiny.resources.loadBalancer.ingress;
  const zeroIngress = unbuffered.resources.loadBalancer.ingress;
  const clients = tiny.workload.clientGroups.reduce((total, group) => total + BigInt(group.count), 0n);

  assert.equal(tinyIngress.queueCapacity, 2);
  assert.equal(zeroIngress.queueCapacity, 0);
  assert.ok(clients > BigInt(tinyIngress.queueCapacity) + 1n);
  const slowerReceiver = BigInt(tinyIngress.bandwidthBitsPerSecond)
    < BigInt(tiny.network.defaults.bandwidthBitsPerSecond);
  assert.ok(slowerReceiver);
  assert.deepEqual(unbuffered.workload, tiny.workload);
  assert.deepEqual({ ...zeroIngress, queueCapacity: 2 }, tinyIngress);
});

test('staggered arrivals retain a scheduled wave beyond the run horizon', () => {
  const scenario = buildPreset('staggered-arrivals');
  const horizon = BigInt(scenario.execution.durationNanoseconds);
  const groups = scenario.workload.clientGroups;
  const starts = groups.map((group) => BigInt(group.startAtNanoseconds));

  assert.equal(starts[0], 0n);
  assert.ok(starts.some((start) => start > 0n && start < horizon));
  assert.equal(starts.at(-1), 200_000_000_000n);
  assert.ok(starts.at(-1)! > horizon);
  assert.ok(groups.every((group) => BigInt(group.clientGapNanoseconds) > 0n));
  assert.equal(new Set(starts).size, groups.length);
});

test('mixed streams exercise zero, partial and segment-boundary payloads without flattening groups', () => {
  const scenario = buildPreset('mixed-streams');
  const short = scenario.workload.clientGroups.find((group) => group.id === 'short-streams')!;
  const long = scenario.workload.clientGroups.find((group) => group.id === 'long-streams')!;

  assert.equal(short.stream.bytes, 4096);
  assert.deepEqual(short.overrides!.map((override) => [override.index, override.stream.bytes]), [
    [0, 0],
    [1, 1],
    [2, 1024],
    [3, 1025],
  ]);
  assert.equal(long.stream.bytes, undefined);
  assert.equal(scenario.workload.defaults.bytes, 1_073_741_824);
});

test('routing and empty-network cases retain their distinguishing native configuration', () => {
  const random = buildPreset('seeded-random');
  assert.equal(random.resources.loadBalancer.algorithm, 'random');
  assert.ok(random.resources.backends.length > 2);
  assert.equal(random.execution.seed, '9007199254740993');
  assert.equal(scenarioFormValues(random).seed, '9007199254740993');

  const missing = buildPreset('no-backends');
  assert.equal(missing.resources.backends.length, 0);
  assert.ok(missing.workload.clientGroups.some((group) => BigInt(group.count) > 0n));

  const idle = buildPreset('idle-network');
  assert.ok(idle.resources.backends.length > 0);
  const idleClients = idle.workload.clientGroups.reduce((total, group) => total + BigInt(group.count), 0n);
  assert.equal(idleClients, 0n);
});
