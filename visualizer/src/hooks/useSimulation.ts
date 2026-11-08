import { useCallback, useEffect, useRef, useState } from 'react';
import { CommandType } from '../commands';
import { scenarioApi, websocketUrl } from '../scenarios/runtimeApi';
import { httpStartAcknowledged } from '../scenarios/startAcknowledgement';
import type { CurrentSimulation, Scenario } from '../scenarios/types';
import { parseSnapshot } from '../telemetry/parse';
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
  runId?: string;
  observedRunIds: Set<string>;
}

interface SimulationLifecycleOptions {
  onRunEnded?: (notification: RunEndedNotification) => void;
  onFinalSample?: (notification: FinalSampleNotification) => void;
}

function newCommandId(): string {
  const randomBytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(randomBytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
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
    let disposed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    const receiver = new TelemetryReceiver(
      (signal) => scenarioApi.topology(signal),
      (view) => {
        if (disposed) return;
        setData(view.data);
        setTopologyLoading(view.loading);
        setTopologyError(view.error);
      },
    );
    telemetryRef.current = receiver;

    function handleCommandError(message: Pick<CommandError, 'commandType' | 'error'>) {
      // A legacy Stop error has no identity and cannot settle this request.
      if (message.commandType === CommandType.StopSimulation) return;
      // Private legacy WS rejection cannot settle an independent HTTP Start.
      if (message.commandType === CommandType.StartSimulation && !httpStartRef.current) {
        setStartPending(false);
      }
      setCommandError(message.error);
    }

    function handleStopResult({ result: message, sample }: ParsedStopCommandResult) {
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
      if (!observation) return;
      setPublicationObservation(observation);
      if (!observation.acceptSnapshot) return;
      applySnapshot(snapshot);
    }

    function handleRunEnded(parsed: ParsedSimulationEnded) {
      // This callback runs synchronously even if React renders only the following run B.
      const receipt = lifecycle.receiveEnd(parsed);
      const { event } = parsed;
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
          const message: unknown = JSON.parse(event.data);
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

  function sendMessage(command: SimulationCommand): boolean {
    const socket = socketRef.current;

    if (!socket || socket.readyState !== WebSocket.OPEN) {
      console.warn('WebSocket not connected. Cannot send message.');
      return false;
    }

    const simulation = simulationRef.current;
    const commandPending = startPendingRef.current || stopCommandsRef.current.pending !== null;
    if (commandPending || !simulation) return false;
    if (simulation.status === 'idle') return false;
    if (command.Type !== CommandType.StopSimulation && simulation.status !== 'running') return false;

    try {
      if (command.Type === CommandType.StopSimulation) {
        return stopCommandsRef.current.send(simulation.runId, newCommandId(), (intent) => {
          setStopPending(true);
          setCommandError(null);
          socket.send(JSON.stringify({ ...command, ...intent }));
        });
      }
      setCommandError(null);
      socket.send(JSON.stringify(command));
      return true;
    } catch (error) {
      setStopPending(false);
      setCommandError('Failed to send the command. Check the server connection.');
      console.error('Cannot send WebSocket command:', error);
      return false;
    }
  }

  async function startScenario(scenario: Scenario): Promise<boolean> {
    const simulation = simulationRef.current;
    const commandPending = startPendingRef.current || stopCommandsRef.current.pending !== null;
    const socketReady = socketRef.current?.readyState === WebSocket.OPEN;
    if (!socketReady || !simulation || simulation.status === 'running' || commandPending) {
      return false;
    }

    const request: HttpStart = {
      controller: new AbortController(),
      observedRunIds: new Set(),
    };
    httpStartRef.current = request;
    setStartPending(true);
    setCommandError(null);

    try {
      const receipt = await scenarioApi.start(scenario, request.controller.signal);
      if (!mountedRef.current || httpStartRef.current !== request) return false;
      request.runId = receipt.runId;
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
    }
  }

  return {
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
    startScenario,
  };
}

export default useSimulation;
