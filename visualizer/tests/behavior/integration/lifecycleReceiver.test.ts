import assert from 'node:assert/strict';
import test from 'node:test';
import { CommandType } from '../../../src/commands.ts';
import { StopCommands, parseStopCommandResult } from '../../../src/hooks/stopCommands.ts';
import { RunLifecycleReceiver } from '../../../src/telemetry/lifecycle.ts';
import type { FinalSampleNotification, RunEndedNotification } from '../../../src/telemetry/lifecycle.ts';
import { parseSimulationEnded } from '../../../src/telemetry/lifecycleParse.ts';
import { TelemetryReceiver } from '../../../src/telemetry/receiver.ts';
import type { SimulationEnded, SimulationSnapshot } from '../../../src/telemetry/types.ts';
import type { StopCommandResult } from '../../../src/types.ts';
import { idleFrame, linkStats, telemetryFrame, topologyDefinition } from '../../telemetryFixture.ts';

function end(reason: SimulationEnded['reason'] = 'finished', runId = 'run-a', sequence = '3'): SimulationEnded {
  const frame = telemetryFrame('2', runId, 3, sequence);
  frame.simulation.status = reason === 'stopped' ? 'running' : reason;
  return {
    type: 'simulationEnded', runId, reason,
    finalSnapshot: frame, topologyDefinition: topologyDefinition('2', runId),
  };
}

function acknowledgement(event: SimulationEnded): StopCommandResult {
  return {
    type: 'commandResult', commandType: CommandType.StopSimulation,
    runId: event.runId, commandId: 'stop-a', status: 'applied',
    finalSnapshot: event.finalSnapshot, topologyDefinition: event.topologyDefinition,
  };
}

function fixture(canDisplayRun?: (runId: string) => boolean) {
  const ends: RunEndedNotification[] = [];
  const samples: FinalSampleNotification[] = [];
  let requests = 0;
  const telemetry = new TelemetryReceiver(() => {
    requests++;
    return new Promise(() => {});
  }, () => {});
  const lifecycle = new RunLifecycleReceiver(
    (event) => ends.push(event),
    (sample) => samples.push(sample),
    canDisplayRun,
  );
  function snapshot(frame: SimulationSnapshot) {
    const observation = lifecycle.observeSnapshot(frame.simulation);
    if (observation?.acceptSnapshot) telemetry.receive(frame);
    return observation;
  }
  function ended(event: SimulationEnded) {
    const receipt = lifecycle.receiveEnd(parseSimulationEnded(event));
    if (receipt.display && receipt.sample) {
      telemetry.receiveFinal(receipt.sample);
    }
    return receipt;
  }
  return { ends, samples, lifecycle, telemetry, snapshot, ended, requests: () => requests };
}

test('end parser requires matching reason, run, revision and a paired self-contained sample', () => {
  for (const reason of ['finished', 'failed', 'stopped'] as const) {
    assert.equal(parseSimulationEnded(end(reason)).event.reason, reason);
  }
  const absent = { ...end('stopped'), finalSnapshot: null, topologyDefinition: null };
  assert.equal(parseSimulationEnded(absent).sample, null);
  for (const invalid of [
    { ...end(), runId: '' },
    { ...end(), commandId: '' },
    { ...end(), reason: ['finished'] },
    { ...end(), reason: 'stopped' },
    { ...absent, reason: 'finished' },
    { ...absent, finalSnapshot: end().finalSnapshot },
    { ...end(), topologyDefinition: topologyDefinition('2', 'run-b') },
    { ...end(), topologyDefinition: topologyDefinition('1', 'run-a') },
    { ...end(), topologyDefinition: { ...topologyDefinition('2', 'run-a'), nodes: [null] } },
    { ...end(), finalSnapshot: { ...end().finalSnapshot, simulation: { ...end().finalSnapshot!.simulation, publicationSequence: '0' } } },
  ]) assert.throws(() => parseSimulationEnded(invalid));
});

test('a fast A end is delivered synchronously before B replaces the displayed state', () => {
  const state = fixture();
  state.snapshot(telemetryFrame('1', 'run-a'));
  const receipt = state.ended(end());
  assert.equal(state.ends.length, 1);
  assert.equal(state.ends[0].sample?.data.simulation.status, 'finished');
  assert.equal(receipt.display, true);
  assert.equal(state.telemetry.state.loading, false);
  assert.equal(state.telemetry.state.data?.topology?.links[0].stats.deliveredBytes, 300);
  assert.equal(state.telemetry.state.data?.topology, receipt.sample?.data.topology);
  assert.equal(state.samples[0].data, receipt.sample?.data);
  assert.equal(state.requests(), 1);

  state.snapshot(telemetryFrame('1', 'run-b'));
  assert.equal(state.telemetry.state.data?.simulation.runId, 'run-b');
  assert.equal(state.ends[0].event.runId, 'run-a');
});

test('an initially idle client displays a fast run end without a running frame or HTTP', () => {
  const state = fixture();
  state.snapshot(idleFrame());
  assert.equal(state.ended(end()).display, true);
  assert.equal(state.ends.length, 1);
  assert.equal(state.telemetry.state.data?.simulation.runId, 'run-a');
  assert.equal(state.telemetry.state.data?.simulation.status, 'finished');
  assert.equal(state.telemetry.state.loading, false);
  assert.equal(state.telemetry.state.data?.topology?.links[0].stats.deliveredBytes, 300);
  assert.equal(state.requests(), 0);
});

test('back-to-back fast ends replace a known ended run without a running frame', () => {
  for (const reason of ['finished', 'stopped'] as const) {
    const state = fixture();
    state.snapshot(idleFrame());
    state.ended(end(reason, 'run-a'));
    assert.equal(state.ended(end('finished', 'run-b')).display, true);
    assert.deepEqual(state.ends.map((notification) => notification.event.runId), ['run-a', 'run-b']);
    assert.equal(state.telemetry.state.data?.simulation.runId, 'run-b');
    assert.equal(state.requests(), 0);
  }
});

test('an end for a cleared run notifies consumers without reactivating the idle projection', () => {
  const stops = new StopCommands();
  const state = fixture((runId) => !stops.hasClearedRun(runId));
  stops.send('run-a', 'stop-a', () => {});
  assert.equal(stops.finish(acknowledgement(end('stopped'))), true);
  state.snapshot(idleFrame());
  assert.equal(state.ended(end('stopped')).display, false);
  assert.equal(state.ends.length, 1);
  assert.equal(state.telemetry.state.data?.simulation.status, 'idle');
  state.lifecycle.disconnect();
  assert.equal(state.lifecycle.terminationUncertainty, null);
});

test('a late A end uses its embedded topology while B stays live and HTTP already describes B', () => {
  const state = fixture();
  state.snapshot(telemetryFrame('1', 'run-a'));
  state.snapshot(telemetryFrame('1', 'run-b'));
  const event = end();
  event.topologyDefinition!.servers[0].name = 'Final A backend';
  const receipt = state.ended(event);
  assert.equal(receipt.display, false);
  assert.equal(state.ends[0].sample?.data.servers[0].server.Name, 'Final A backend');
  assert.equal(state.ends[0].sample?.data.simulation.runId, 'run-a');
  assert.equal(state.telemetry.state.data?.simulation.runId, 'run-b');
  assert.equal(state.snapshot(telemetryFrame('2', 'run-a', 3, '3')), null);
  assert.equal(state.requests(), 2);
});

test('duplicate ends notify once, and reconnect cached duplicates still bootstrap the UI', () => {
  const state = fixture();
  const event = end();
  state.ended(event);
  assert.equal(state.ended(event).display, false);
  assert.equal(state.ends.length, 1);
  assert.equal(state.samples.length, 1);
  state.lifecycle.reconnect();
  state.telemetry.reset();
  const cached = state.ended(event);
  assert.equal(cached.display, true);
  assert.equal(cached.publicationObservation?.newPublication, false);
  assert.equal(state.telemetry.state.data?.simulation.status, 'finished');
  assert.equal(state.telemetry.state.loading, false);
  assert.equal(state.requests(), 0);
  assert.equal(state.ends.length, 1);
  assert.equal(state.samples.length, 1);
});

test('an ordinary final publication is not processed twice when its end supplies topology', () => {
  const state = fixture();
  const event = end();
  state.snapshot(event.finalSnapshot!);
  const receipt = state.ended(event);
  assert.equal(receipt.publicationObservation?.kind, 'duplicate');
  assert.equal(receipt.publicationObservation?.newPublication, false);
  assert.equal(state.ends.length, 1);
  assert.equal(state.samples.length, 0);
  assert.equal(state.telemetry.state.loading, false);
  assert.equal(state.telemetry.state.data?.topology?.links[0].stats.deliveredBytes, 300);
});

test('running Stop delivers its final sample before the matching private acknowledgement clears intent', () => {
  const stops = new StopCommands();
  const order: string[] = [];
  const lifecycle = new RunLifecycleReceiver(() => {
    assert.ok(stops.pending);
    order.push('end');
  }, () => {
    assert.ok(stops.pending);
    order.push('sample');
  });
  stops.send('run-a', 'stop-a', () => {});
  lifecycle.observeSnapshot(telemetryFrame('1', 'run-a').simulation);
  const event = end('stopped');
  lifecycle.receiveEnd(parseSimulationEnded(event));
  lifecycle.observeSnapshot(idleFrame().simulation);
  const ack = acknowledgement(event);
  const parsedAck = parseStopCommandResult(ack);
  assert.ok(parsedAck);
  lifecycle.receiveStopAcknowledgement(parsedAck.sample);
  assert.equal(stops.finish(ack), true);
  order.push('ack');
  assert.deepEqual(order, ['sample', 'end', 'ack']);
  assert.equal(stops.pending, null);
});

test('Stop before world creation permits a null final sample and still notifies one end', () => {
  const state = fixture();
  const event = { ...end('stopped'), commandId: 'stop-a', finalSnapshot: null, topologyDefinition: null };
  assert.equal(state.ended(event).sample, null);
  state.snapshot(idleFrame());
  state.ended(event);
  assert.equal(state.ends.length, 1);
  assert.equal(state.samples.length, 0);
  assert.equal(state.telemetry.state.data?.simulation.status, 'idle');
  assert.ok(parseStopCommandResult(acknowledgement(event)));
});

test('clearing a retained terminal run can deliver its sample without creating another end', () => {
  const state = fixture();
  const event = end('failed');
  state.ended(event);
  state.snapshot(idleFrame());
  const parsedAck = parseStopCommandResult(acknowledgement(event));
  assert.ok(parsedAck);
  state.lifecycle.receiveStopAcknowledgement(parsedAck.sample);
  assert.equal(state.ends.length, 1);
  assert.equal(state.samples.length, 1);

  const attached = fixture();
  attached.snapshot(idleFrame());
  attached.lifecycle.receiveStopAcknowledgement(parsedAck.sample);
  assert.equal(attached.ends.length, 0);
  assert.equal(attached.samples[0].source, 'stopAcknowledgement');
  assert.equal(attached.samples[0].data, parsedAck.sample?.data);
  assert.equal(attached.telemetry.state.data?.simulation.status, 'idle');
});

test('invalid Stop acknowledgement samples cannot clear an outstanding command', () => {
  const stops = new StopCommands();
  stops.send('run-a', 'stop-a', () => {});
  const event = end();
  const ack = acknowledgement(event);
  for (const invalid of [
    { ...ack, topologyDefinition: undefined },
    { ...ack, topologyDefinition: topologyDefinition('2', 'run-b') },
    { ...ack, finalSnapshot: null },
    { ...ack, status: 'rejected' },
  ]) assert.equal(parseStopCommandResult(invalid), null);
  assert.deepEqual(stops.pending, { runId: 'run-a', commandId: 'stop-a' });
});

test('malformed final measurements and metadata cannot notify an end or clear a Stop intent', () => {
  const stops = new StopCommands();
  stops.send('run-a', 'stop-a', () => {});
  const event = end();
  const frame = event.finalSnapshot!;
  const { topology } = frame;
  const duration = { samples: 1, meanSeconds: 0, minSeconds: 0, maxSeconds: 0 };
  const metadata = {
    error: 'Reported simulation failure',
    performance: {
      wallTimeSeconds: 0, eventsExecuted: 0, eventsPerWallSecond: 0,
      forwardedPackets: 0, forwardedPacketsPerWallSecond: 0,
    },
    payload: { receivedBytes: 0, acknowledgedBytes: 0 },
    transfers: {
      started: 1, notStarted: 0, succeeded: 1, failed: 0, inProgress: 0,
      failures: {}, synRetransmissions: 0, dataRetransmissions: 0,
      handshake: duration, payload: duration, completion: duration,
    },
  };
  const validFrame = { ...frame, simulation: { ...frame.simulation, ...metadata } };
  assert.equal(parseSimulationEnded({ ...event, finalSnapshot: validFrame }).sample?.data.simulation.transfers, metadata.transfers);
  const invalidMetadata = [
    { error: {} },
    { performance: null },
    { performance: { ...metadata.performance, wallTimeSeconds: 'bad' } },
    { payload: null },
    { payload: { ...metadata.payload, acknowledgedBytes: {} } },
    { transfers: null },
    { transfers: { ...metadata.transfers, started: 'bad' } },
    { transfers: { ...metadata.transfers, failures: null } },
    { transfers: { ...metadata.transfers, failures: { dataRetryLimit: 'bad' } } },
    { transfers: { ...metadata.transfers, handshake: null } },
    { transfers: { ...metadata.transfers, handshake: { ...duration, meanSeconds: null } } },
    { transfers: { ...metadata.transfers, payload: { ...duration, minSeconds: {} } } },
    { transfers: { ...metadata.transfers, completion: { ...duration, maxSeconds: 'bad' } } },
  ];
  const invalidStats = [undefined, null, {}, { ...linkStats(), queuedPackets: 'bad' }, { ...linkStats(), transmitting: 0 }];
  const invalidFrames = invalidStats.flatMap((stats) => [
    { ...frame, topology: { ...topology, links: [{ ...topology.links[0], stats }, ...topology.links.slice(1)] } },
    { ...frame, topology: { ...topology, ingress: { ...topology.ingress, stats } } },
    { ...frame, topology: { ...topology, ingress: { ...topology.ingress, queues: [
      { ...topology.ingress!.queues[0], stats }, ...topology.ingress!.queues.slice(1),
    ] } } },
  ]);
  const invalidResources = [
    ...invalidFrames,
    ...invalidMetadata.map((simulation) => ({ ...frame, simulation: { ...frame.simulation, ...simulation } })),
    { ...frame, topology: { ...topology, ingress: null } },
    { ...frame, topology: { ...topology, nodes: [{ ...topology.nodes[0], metrics: null }, ...topology.nodes.slice(1)] } },
    { ...frame, topology: { ...topology, nodes: [{ ...topology.nodes[0], metrics: { packets: 'bad' } }, ...topology.nodes.slice(1)] } },
    { ...frame, servers: [{ ...frame.servers[0], clients: null }] },
    { ...frame, servers: [{ ...frame.servers[0], clients: [null] }] },
  ];
  for (const finalSnapshot of invalidResources) {
    assert.throws(() => parseSimulationEnded({ ...event, finalSnapshot }), /malformed/);
    const parsed = parseStopCommandResult({ ...acknowledgement(event), finalSnapshot });
    if (parsed) stops.finish(parsed.result);
    assert.deepEqual(stops.pending, { runId: 'run-a', commandId: 'stop-a' });
    assert.equal(parsed, null);
  }
});

test('disconnect preserves unknown termination across reconnect even without a pending Stop', () => {
  const state = fixture();
  state.snapshot(telemetryFrame('1', 'run-a'));
  state.lifecycle.disconnect();
  assert.equal(state.lifecycle.terminationUncertainty, 'run-a');
  state.lifecycle.reconnect();
  state.snapshot(telemetryFrame('1', 'run-b'));
  assert.equal(state.lifecycle.terminationUncertainty, 'run-a');
  state.lifecycle.disconnect();
  assert.equal(state.lifecycle.terminationUncertainty, 'run-a');
  state.snapshot(telemetryFrame('1', 'run-b'));
  assert.equal(state.ends.length, 0);
  state.ended(end('finished', 'run-b'));
  assert.equal(state.lifecycle.terminationUncertainty, 'run-a');
  state.ended(end('finished', 'run-a'));
  assert.equal(state.lifecycle.terminationUncertainty, null);
  assert.equal(state.telemetry.state.data?.simulation.runId, 'run-b');
  assert.equal(state.ended(end('finished', 'run-c')).display, true);
  assert.equal(state.telemetry.state.data?.simulation.runId, 'run-c');
  const ended = fixture();
  ended.ended(end('stopped'));
  ended.lifecycle.disconnect();
  assert.equal(ended.lifecycle.terminationUncertainty, null);
});
