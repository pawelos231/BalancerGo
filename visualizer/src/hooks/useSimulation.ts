import { useCallback, useEffect, useRef, useState } from 'react';
import { CommandType } from '../commands';
import { scenarioApi, websocketUrl } from '../scenarios/runtimeApi';
import { httpStartAcknowledged } from '../scenarios/startAcknowledgement';
import type { CurrentSimulation, Scenario } from '../scenarios/types';
import type {
  CommandError,
  LoadBalancerState,
  SimulationCommand,
  SimulationMetadata,
} from '../types';

const reconnectDelayMs = 1000;

interface HttpStart {
  controller: AbortController;
  runId?: string;
  observedRunIds: Set<string>;
}

function isCommandError(
  message: LoadBalancerState | CommandError,
): message is CommandError {
  return 'type' in message && message.type === 'commandError';
}

function useSimulation() {
  const [data, setData] = useState<LoadBalancerState | null>(null);
  const [connected, setConnected] = useState(false);
  const [connectionEpoch, setConnectionEpoch] = useState(0);
  const [startPending, setStartPendingState] = useState(false);
  const [stopPending, setStopPendingState] = useState(false);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [currentRun, setCurrentRun] = useState<CurrentSimulation | null>(null);
  const [currentRunError, setCurrentRunError] = useState<string | null>(null);
  const [runRefresh, setRunRefresh] = useState(0);

  const socketRef = useRef<WebSocket | null>(null);
  const simulationRef = useRef<SimulationMetadata | null>(null);
  const startPendingRef = useRef(false);
  const stopPendingRef = useRef(false);
  const httpStartRef = useRef<HttpStart | null>(null);
  const mountedRef = useRef(false);

  // Refs guard repeated commands before React updates the buttons.
  const setStartPending = useCallback((pending: boolean) => {
    startPendingRef.current = pending;
    setStartPendingState(pending);
  }, []);

  const setStopPending = useCallback((pending: boolean) => {
    stopPendingRef.current = pending;
    setStopPendingState(pending);
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    let disposed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;

    function handleCommandError(message: CommandError) {
      setCommandError(message.error);

      if (message.commandType === CommandType.StopSimulation) {
        setStopPending(false);
      }
    }

    function handleSnapshot(snapshot: LoadBalancerState) {
      const hasConfiguration = Boolean(snapshot.simulation?.config);
      const hasServers = Array.isArray(snapshot.servers);

      if (!hasConfiguration || !hasServers) {
        console.error('Snapshot is missing simulation metadata.');
        return;
      }

      simulationRef.current = snapshot.simulation;
      setData(snapshot);
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

      // Only an idle snapshot acknowledges Stop and clear.
      if (snapshot.simulation.status === 'idle') {
        setStopPending(false);
      }
    }

    function connect() {
      const socket = new WebSocket(websocketUrl);
      socketRef.current = socket;

      socket.onopen = () => {
        if (disposed) return;

        // Wait for a fresh snapshot before enabling controls after reconnect.
        simulationRef.current = null;
        if (!httpStartRef.current) setStartPending(false);
        setStopPending(false);
        setCommandError(null);
        setConnectionEpoch((epoch) => epoch + 1);
        setConnected(false);
      };

      socket.onmessage = (event) => {
        if (disposed) return;

        try {
          const message = JSON.parse(event.data) as LoadBalancerState | CommandError;

          if (isCommandError(message)) {
            handleCommandError(message);
          } else {
            handleSnapshot(message);
          }
        } catch (error) {
          console.error('Error parsing JSON:', error);
        }
      };

      socket.onclose = () => {
        if (disposed) return;

        simulationRef.current = null;
        if (!httpStartRef.current) setStartPending(false);
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
      httpStartRef.current?.controller.abort();
      httpStartRef.current = null;
      clearTimeout(reconnectTimer);
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [setStartPending, setStopPending]);

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

  function reserveCommand(command: SimulationCommand): boolean {
    const simulation = simulationRef.current;

    switch (command.Type) {
      case CommandType.StopSimulation:
        if (!simulation || simulation.status === 'idle') return false;

        setStopPending(true);
        return true;

      default:
        return simulation?.status === 'running';
    }
  }

  function sendMessage(command: SimulationCommand): boolean {
    const socket = socketRef.current;

    if (!socket || socket.readyState !== WebSocket.OPEN) {
      console.warn('WebSocket not connected. Cannot send message.');
      return false;
    }

    const commandPending = startPendingRef.current || stopPendingRef.current;

    if (commandPending || !reserveCommand(command)) {
      return false;
    }

    setCommandError(null);

    try {
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
    const commandPending = startPendingRef.current || stopPendingRef.current;
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
    refreshCurrent,
    sendMessage,
    startScenario,
  };
}

export default useSimulation;
