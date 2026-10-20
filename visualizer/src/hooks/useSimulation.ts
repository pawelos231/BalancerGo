import { useCallback, useEffect, useRef, useState } from 'react';
import { CommandType } from '../commands';
import type {
  CommandError,
  LoadBalancerState,
  SimulationCommand,
  SimulationMetadata,
} from '../types';

const websocketUrl = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws';
const reconnectDelayMs = 1000;

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

  const socketRef = useRef<WebSocket | null>(null);
  const simulationRef = useRef<SimulationMetadata | null>(null);
  const startPendingRef = useRef(false);
  const stopPendingRef = useRef(false);

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
    let disposed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;

    function handleCommandError(message: CommandError) {
      setCommandError(message.error);

      if (message.commandType === CommandType.StartSimulation) {
        setStartPending(false);
      } else if (message.commandType === CommandType.StopSimulation) {
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

      // A fast run can finish before its running snapshot arrives.
      setStartPending(false);

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
        setData(null);
        setStartPending(false);
        setStopPending(false);
        setCommandError(null);
        setConnectionEpoch((epoch) => epoch + 1);
        setConnected(true);
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
        setStartPending(false);
        setStopPending(false);
        setConnected(false);
        reconnectTimer = setTimeout(connect, reconnectDelayMs);
      };

      socket.onerror = () => socket.close();
    }

    connect();

    return () => {
      disposed = true;
      clearTimeout(reconnectTimer);
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [setStartPending, setStopPending]);

  function reserveCommand(command: SimulationCommand): boolean {
    const simulation = simulationRef.current;

    switch (command.Type) {
      case CommandType.StartSimulation:
        if (!simulation || simulation.status === 'running') return false;

        setStartPending(true);
        return true;

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
      setStartPending(false);
      setStopPending(false);
      setCommandError('Failed to send the command. Check the server connection.');
      console.error('Cannot send WebSocket command:', error);
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
    sendMessage,
  };
}

export default useSimulation;
