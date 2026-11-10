import { useCallback, useEffect, useRef, useState } from 'react';
import { CommandType } from '../commands';
import { ResultsCollection } from '../results/collection';
import { ResultsSession } from '../results/session';
import type { StopResultMode } from '../results/session';
import { scenarioApi, websocketUrl } from '../scenarios/runtimeApi';
import { httpStartAcknowledged } from '../scenarios/startAcknowledgement';
import type { CurrentSimulation, Scenario } from '../scenarios/types';
import { parseSnapshot } from '../telemetry/parse';
import { readTelemetryJSON } from '../telemetry/exact';
import type { PublicationObservation } from '../telemetry/publications';
import { RunLifecycleReceiver } from '../telemetry/lifecycle';
import type { FinalSampleNotification, RunEndedNotification } from '../telemetry/lifecycle';
import { parseSimulationEnded } from '../telemetry/lifecycleParse';
import type { FinalSample, ParsedSimulationEnded } from '../telemetry/lifecycleParse';
import { TelemetryReceiver } from '../telemetry/receiver';
import type { SimulationSnapshot } from '../telemetry/types';
import type {
  CommandError,
  LoadBalancerState,
  SimulationCommand,
  SimulationMetadata,
} from '../types';
import { parseStopCommandResult, StopCommands } from './stopCommands';
import type { ParsedStopCommandResult } from './stopCommands';
import { readCommandError } from './commandErrors';

const reconnectDelayMs = 1000;
const uncertainStopError = 'Connection lost before Stop was acknowledged. '
  + 'Its outcome is unknown; check the current run before trying again.';

interface HttpStart {
  controller: AbortController;
  scenario: Scenario;
  runId?: string;
  observedRunIds: Set<string>;
  settled: Promise<void>;
  markSettled: () => void;
}

interface SimulationLifecycleOptions {
  onRunEnded?: (notification: RunEndedNotification) => void;
  onFinalSample?: (notification: FinalSampleNotification) => void;
}

function newCommandId(): string {
  const randomBytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(randomBytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

// Result finalization waits until this request has attached its admitted input.
function newHttpStart(scenario: Scenario): HttpStart {
  let markSettled!: () => void;
  const settled = new Promise<void>((resolve) => { markSettled = resolve; });
  return {
    controller: new AbortController(), scenario: structuredClone(scenario),
    observedRunIds: new Set(), settled, markSettled,
  };
}

function useSimulation(options: SimulationLifecycleOptions = {}) {
  const [data, setData] = useState<LoadBalancerState | null>(null);
  const [connected, setConnected] = useState(false);
  const [connectionEpoch, setConnectionEpoch] = useState(0);
  const [startPending, setStartPendingState] = useState(false);
  const [stopPending, setStopPending] = useState(false);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [currentRun, setCurrentRun] = useState<CurrentSimulation | null>(null);
  const [currentRunError, setCurrentRunError] = useState<string | null>(null);
  const [runRefresh, setRunRefresh] = useState(0);
  const [topologyLoading, setTopologyLoading] = useState(false);
  const [topologyError, setTopologyError] = useState<string | null>(null);
  const [publicationObservation, setPublicationObservation] = useState<PublicationObservation | null>(null);
  const [terminationUncertainty, setTerminationUncertainty] = useState<string | null>(null);

  const socketRef = useRef<WebSocket | null>(null);
  const simulationRef = useRef<SimulationMetadata | null>(null);
  const startPendingRef = useRef(false);
  const stopCommandsRef = useRef(new StopCommands());
  const httpStartRef = useRef<HttpStart | null>(null);
  const mountedRef = useRef(false);
  const telemetryRef = useRef<TelemetryReceiver | null>(null);
  const resultsRef = useRef<ResultsCollection | null>(null);
  if (!resultsRef.current) resultsRef.current = new ResultsCollection();
  const resultSessionRef = useRef<ResultsSession | null>(null);
  if (!resultSessionRef.current) {
    const collection = resultsRef.current;
    resultSessionRef.current = new ResultsSession(collection, {
      prepareMetadata: async (runId, signal) => {
        const startSettled = httpStartRef.current?.settled;
        const context = scenarioApi.current(signal).then((current) => {
          if (current?.runId === runId && resultSessionRef.current!.acceptsRun(runId)) {
            collection.metadata(current);
          }
        });
        // A fast run may finish before its Start receipt or current document arrives.
        await Promise.allSettled([context, startSettled]);
      },
    });
  }
  const lifecycleOptionsRef = useRef(options);
  lifecycleOptionsRef.current = options;
  const lifecycleRef = useRef<RunLifecycleReceiver | null>(null);
  if (!lifecycleRef.current) {
    lifecycleRef.current = new RunLifecycleReceiver(
      (notification) => lifecycleOptionsRef.current.onRunEnded?.(notification),
      (notification) => lifecycleOptionsRef.current.onFinalSample?.(notification),
      (runId) => !stopCommandsRef.current.hasClearedRun(runId),
    );
  }

  // Refs guard repeated commands before React updates the buttons.
  const setStartPending = useCallback((pending: boolean) => {
    startPendingRef.current = pending;
    setStartPendingState(pending);
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    const stopCommands = stopCommandsRef.current;
    const lifecycle = lifecycleRef.current!;
    const results = resultsRef.current!;
    const resultSession = resultSessionRef.current!;
    let disposed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    const receiver = new TelemetryReceiver(
      async (signal) => {
        const definition = await scenarioApi.topology(signal);
        if (resultSession.acceptsRun(definition.runId)) results.definition(definition);
        return definition;
      },
      (view) => {
        if (disposed) return;
        setData(view.data);
        setTopologyLoading(view.loading);
        setTopologyError(view.error);
      },
    );
    telemetryRef.current = receiver;

    // End messages bring their own definition, even if this publication was already seen.
    function collectFinal(sample: FinalSample | null) {
      if (!sample || !resultSession.acceptsRun(sample.definition.runId)) return;
      const receivedAt = Date.now();
      results.definition(sample.definition, receivedAt, true);
      results.snapshot(sample.snapshot, receivedAt, true);
    }

    function handleCommandError(message: Pick<CommandError, 'commandType' | 'error'>) {
      // A legacy Stop error has no identity and cannot settle this request.
      if (message.commandType === CommandType.StopSimulation) return;
      setCommandError(message.error);
    }

    function handleStopResult({ result: message, sample }: ParsedStopCommandResult) {
      collectFinal(sample);
      // Results retains its own addressed intent across a disconnect or a newer run.
      resultSession.receiveStopResult(message);
      const pending = stopCommands.pending;
      if (pending?.runId !== message.runId || pending.commandId !== message.commandId) return;
      // Deliver the validated sample before finish retires this run and clears its intent.
      lifecycle.receiveStopAcknowledgement(sample);
      if (!stopCommands.finish(message)) return;
      setStopPending(false);
      if (message.status === 'rejected') {
        setCommandError(message.error || 'The server rejected the stop command.');
        return;
      }

      setCommandError(null);
      setCurrentRun((current) => current?.runId === message.runId ? null : current);
      setRunRefresh((revision) => revision + 1);
      const simulation = simulationRef.current;
      if (simulation?.runId !== message.runId || simulation.status === 'idle') return;

      // Replies can precede idle telemetry. Require a fresh view before another Start.
      simulationRef.current = null;
      receiver.reset();
      setData(null);
      setCurrentRunError(null);
      setConnected(false);
    }

    function applySnapshot(snapshot: SimulationSnapshot, sample?: FinalSample) {
      simulationRef.current = snapshot.simulation;
      setConnected(true);

      // HTTP needs its own run ID; an older idle frame cannot acknowledge Start.
      const httpStart = httpStartRef.current;
      if (httpStart && snapshot.simulation.runId) {
        httpStart.observedRunIds.add(snapshot.simulation.runId);
      }
      const matchesHttpStart = httpStart?.runId !== undefined
        && httpStart.runId === snapshot.simulation.runId;
      if (!httpStart || matchesHttpStart) {
        httpStartRef.current = null;
        setStartPending(false);
      } else if (httpStart.runId && snapshot.simulation.status === 'idle') {
        // Coalescing may replace running with idle without changing effect dependencies.
        setRunRefresh((revision) => revision + 1);
      }

      if (sample) receiver.receiveFinal(sample);
      else receiver.receive(snapshot);
    }

    function handleSnapshot(snapshot: SimulationSnapshot) {
      // Reject retired runs before tracking so cleared runs cannot become current again.
      if (!stopCommands.acceptsSnapshot(snapshot.simulation)) return;
      const observation = lifecycle.observeSnapshot(snapshot.simulation);
      if (observation?.acceptSnapshot && snapshot.simulation.status !== 'idle') {
        resultSession.observeRun(snapshot.simulation.runId);
      }
      // Keep unique receipts before rendering, including old publications rejected by the view.
      if (snapshot.simulation.runId && resultSession.acceptsRun(snapshot.simulation.runId)) {
        results.snapshot(snapshot);
      }
      if (!observation) return;
      setPublicationObservation(observation);
      if (!observation.acceptSnapshot) return;
      applySnapshot(snapshot);
    }

    function handleRunEnded(parsed: ParsedSimulationEnded) {
      collectFinal(parsed.sample);
      // This callback runs synchronously even if React renders only the following run B.
      const receipt = lifecycle.receiveEnd(parsed);
      const { event } = parsed;
      if (receipt.display) {
        resultSession.observeRun(event.runId);
        // Adopting B may have freed an older failed collection; retry its final source.
        collectFinal(parsed.sample);
      }
      resultSession.receiveEnd(event);
      if (!lifecycle.terminationUncertainty) setTerminationUncertainty(null);
      const httpStart = httpStartRef.current;
      httpStart?.observedRunIds.add(event.runId);
      if (httpStart?.runId === event.runId) {
        httpStartRef.current = null;
        setStartPending(false);
      }
      if (!receipt.display || !receipt.sample
        || !stopCommands.acceptsSnapshot(receipt.sample.snapshot.simulation)) return;
      setPublicationObservation(receipt.publicationObservation);
      applySnapshot(receipt.sample.snapshot, receipt.sample);
    }

    function connect() {
      const socket = new WebSocket(websocketUrl);
      socketRef.current = socket;

      socket.onopen = () => {
        if (disposed) return;

        // Wait for a fresh snapshot before enabling controls after reconnect.
        simulationRef.current = null;
        results.reconnected();
        lifecycle.reconnect();
        receiver.reset();
        setCurrentRun(null);
        setCurrentRunError(null);
        if (!httpStartRef.current) setStartPending(false);
        setStopPending(false);
        setCommandError((error) => error === uncertainStopError ? error : null);
        setConnectionEpoch((epoch) => epoch + 1);
        setConnected(false);
      };

      socket.onmessage = (event) => {
        if (disposed) return;

        try {
          const message: unknown = readTelemetryJSON(event.data);
          const error = readCommandError(message);
          const stopResult = parseStopCommandResult(message);

          if (error) {
            handleCommandError(error);
          } else if (stopResult) {
            handleStopResult(stopResult);
          } else if (message !== null && typeof message === 'object'
            && 'type' in message && message.type === 'simulationEnded') {
            handleRunEnded(parseSimulationEnded(message));
          } else {
            handleSnapshot(parseSnapshot(message));
          }
        } catch (error) {
          setCommandError(error instanceof Error ? error.message : 'Cannot read the live simulation state.');
          console.error('Cannot read the live simulation state:', error);
        }
      };

      socket.onclose = () => {
        if (disposed) return;

        simulationRef.current = null;
        results.disconnected();
        resultSession.disconnected();
        lifecycle.disconnect();
        const uncertainRunId = lifecycle.terminationUncertainty;
        if (uncertainRunId) {
          setTerminationUncertainty(`Connection lost while run ${uncertainRunId} was active. `
            + 'Its termination is unknown; reconnecting does not guarantee replay of its end event.');
        }
        receiver.reset();
        if (!httpStartRef.current) setStartPending(false);
        if (stopCommands.disconnect()) setCommandError(uncertainStopError);
        setStopPending(false);
        setConnected(false);
        reconnectTimer = setTimeout(connect, reconnectDelayMs);
      };

      socket.onerror = () => socket.close();
    }

    connect();

    return () => {
      disposed = true;
      mountedRef.current = false;
      receiver.dispose();
      if (telemetryRef.current === receiver) telemetryRef.current = null;
      httpStartRef.current?.controller.abort();
      httpStartRef.current = null;
      stopCommands.disconnect();
      clearTimeout(reconnectTimer);
      socketRef.current?.close();
      socketRef.current = null;
      // Strict Mode repeats setup immediately; dispose only after a real unmount.
      queueMicrotask(() => {
        if (!mountedRef.current) resultSession.dispose();
      });
    };
  }, [setStartPending]);

  const runId = data?.simulation.runId;
  const runStatus = data?.simulation.status;

  // Read the exact document over HTTP, separately from the live frames.
  useEffect(() => {
    if (!connected || !runStatus) return;
    const controller = new AbortController();
    const requestRunId = simulationRef.current?.runId;
    const pendingStart = httpStartRef.current;
    const acceptedRunId = pendingStart?.runId;

    scenarioApi.current(controller.signal).then((current) => {
      if (controller.signal.aborted) return;
      // Metadata belongs to its returned run, even if the live view already shows another one.
      if (current && resultSessionRef.current!.acceptsRun(current.runId)) {
        resultsRef.current!.metadata(current);
      }
      if (current && stopCommandsRef.current.hasClearedRun(current.runId)) return;
      const latestSnapshot = simulationRef.current;
      const snapshotChanged = latestSnapshot?.runId !== requestRunId;
      if (snapshotChanged && current?.runId !== latestSnapshot?.runId) return;
      setCurrentRun(current);
      setCurrentRunError(null);

      const stillPending = acceptedRunId !== undefined && httpStartRef.current === pendingStart;
      if (stillPending && httpStartAcknowledged(acceptedRunId, current, latestSnapshot)) {
        httpStartRef.current = null;
        setStartPending(false);
      }
    }).catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setCurrentRunError(error instanceof Error ? error.message : 'Cannot load the current scenario.');
    });

    return () => controller.abort();
  }, [connected, connectionEpoch, runId, runStatus, runRefresh, setStartPending]);

  const refreshCurrent = useCallback(() => {
    setRunRefresh((revision) => revision + 1);
  }, []);

  const refreshTopology = useCallback(() => {
    telemetryRef.current?.retry();
  }, []);

  function sendMessage(command: SimulationCommand, mode: StopResultMode = 'clear'): boolean {
    const socket = socketRef.current;

    if (!socket || socket.readyState !== WebSocket.OPEN) {
      console.warn('WebSocket not connected. Cannot send message.');
      return false;
    }

    const simulation = simulationRef.current;
    const commandPending = startPendingRef.current || stopCommandsRef.current.pending !== null;
    if (commandPending || !simulation) return false;
    if (simulation.status === 'idle') return false;

    const intent = { runId: simulation.runId, commandId: newCommandId() };
    try {
      if (command.Type === CommandType.StopSimulation) {
        const session = resultSessionRef.current!;
        if (!session.requestStop(intent, mode)) {
          setCommandError(session.state.warning || 'Wait for the previous Stop to be confirmed.');
          return false;
        }
        const sent = stopCommandsRef.current.send(intent.runId, intent.commandId, (address) => {
          setStopPending(true);
          setCommandError(null);
          socket.send(JSON.stringify({ ...command, ...address }));
        });
        if (!sent) session.cancelStop(intent);
        return sent;
      }
      return false;
    } catch (error) {
      resultSessionRef.current!.cancelStop(intent);
      setStopPending(false);
      setCommandError('Failed to send the command. Check the server connection.');
      console.error('Cannot send WebSocket command:', error);
      return false;
    }
  }

  function stopAndSaveResults(): boolean {
    return sendMessage({ Type: CommandType.StopSimulation, Payload: {} }, 'save');
  }

  async function startScenario(scenario: Scenario): Promise<boolean> {
    const simulation = simulationRef.current;
    const commandPending = startPendingRef.current || stopCommandsRef.current.pending !== null;
    const socketReady = socketRef.current?.readyState === WebSocket.OPEN;
    if (!socketReady || !simulation || simulation.status === 'running' || commandPending) {
      return false;
    }

    const request = newHttpStart(scenario);
    httpStartRef.current = request;
    setStartPending(true);
    setCommandError(null);

    try {
      const receipt = await scenarioApi.start(request.scenario, request.controller.signal);
      if (!mountedRef.current || httpStartRef.current !== request) return false;
      request.runId = receipt.runId;
      // Keep the admitted input separately from any edits made while the request was in flight.
      if (resultSessionRef.current!.acceptsRun(receipt.runId)) {
        resultsRef.current!.submitted(receipt.runId, request.scenario);
      }
      setRunRefresh((revision) => revision + 1);

      // A speed-0 run may have already finished while its HTTP receipt was in flight.
      if (request.observedRunIds.has(receipt.runId)) {
        httpStartRef.current = null;
        setStartPending(false);
      }
      return true;
    } catch (error) {
      if (!mountedRef.current || httpStartRef.current !== request) return false;
      httpStartRef.current = null;
      setStartPending(false);
      setCommandError(error instanceof Error ? error.message : 'Failed to start the scenario.');
      return false;
    } finally {
      request.markSettled();
    }
  }

  return {
    resultsSession: resultSessionRef.current,
    data,
    connected,
    connectionEpoch,
    startPending,
    stopPending,
    commandError,
    currentRun,
    currentRunError,
    topologyLoading,
    topologyError,
    publicationObservation,
    terminationUncertainty,
    refreshTopology,
    refreshCurrent,
    sendMessage,
    stopAndSaveResults,
    startScenario,
  };
}

export default useSimulation;
