import {
  AppBar, Box, Chip, Container, CssBaseline, FormControlLabel, LinearProgress,
  Stack, Switch, Tab, Tabs, ThemeProvider, Toolbar, Typography, createTheme,
} from '@mui/material';
import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { Link as RouterLink, Navigate, Route, Routes, useLocation } from 'react-router';
import SimulationPage from './pages/SimulationPage';
import ControlPanel from './components/ControlPanel';
import HelpTooltip from './components/HelpTooltip';
import { CommandType } from './commands';
import type { CommandError, LoadBalancerState, SimulationConfig, SimulationMetadata } from './types';

export type Payload =
  | { Type: CommandType.SetAlgorithm; Payload: { Name: string } }
  | { Type: CommandType.SpawnClient; Payload: { Count: number } }
  | { Type: CommandType.StartSimulation; Payload: SimulationConfig }
  | { Type: CommandType.StopSimulation; Payload: Record<string, never> };

const TopologyPage = lazy(() => import('./pages/TopologyPage'));

function App() {
  const { pathname } = useLocation();
  const [data, setData] = useState<LoadBalancerState | null>(null);
  const [connected, setConnected] = useState(false);
  const [connectionEpoch, setConnectionEpoch] = useState(0);
  const [startPending, setStartPending] = useState(false);
  const [stopPending, setStopPending] = useState(false);
  const [commandError, setCommandError] = useState<string | null>(null);
  const startPendingRef = useRef(false);
  const stopPendingRef = useRef(false);
  const simulationRef = useRef<SimulationMetadata | null>(null);
  const [darkMode, setDarkMode] = useState(() => {
    if (typeof window === 'undefined') {
      return false;
    }
    const storedMode = window.localStorage.getItem('lb-dark-mode');
    if (storedMode === null) {
      return false;
    }
    return storedMode === 'true';
  });
  const wsRef = useRef<WebSocket | null>(null);

  const theme = createTheme({
    palette: {
      mode: darkMode ? 'dark' : 'light',
      primary: {
        main: darkMode ? '#7dd3fc' : '#0ea5e9',
      },
      secondary: {
        main: darkMode ? '#a5b4fc' : '#6366f1',
      },
      background: {
        default: darkMode ? '#0f172a' : '#f7f9fc',
        paper: darkMode ? '#111827' : '#ffffff',
      },
    },
  });

  useEffect(() => {
    let disposed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;

    function connect() {
      const ws = new WebSocket(import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws');
      wsRef.current = ws;

      ws.onopen = () => {
        if (disposed) return;
        // Reconnect must receive a fresh snapshot before exposing controls.
        simulationRef.current = null;
        startPendingRef.current = false;
        stopPendingRef.current = false;
        setData(null);
        setStartPending(false);
        setStopPending(false);
        setCommandError(null);
        setConnectionEpoch((epoch) => epoch + 1);
        setConnected(true);
      };

      ws.onmessage = (event) => {
        if (disposed) return;
        try {
          const incoming = JSON.parse(event.data) as LoadBalancerState | CommandError;
          if ('type' in incoming && incoming.type === 'commandError') {
            setCommandError(incoming.error);
            if (incoming.commandType === CommandType.StartSimulation) {
              startPendingRef.current = false;
              setStartPending(false);
            } else if (incoming.commandType === CommandType.StopSimulation) {
              stopPendingRef.current = false;
              setStopPending(false);
            }
            return;
          }
          const message = incoming as LoadBalancerState;
          if (!message.simulation?.config || !Array.isArray(message.servers)) {
            console.error('Snapshot is missing simulation metadata.');
            return;
          }
          simulationRef.current = message.simulation;
          setData(message);
          // A fast experiment may finish before a running frame is delivered.
          startPendingRef.current = false;
          setStartPending(false);
          // In-flight running/finished frames are not stop acknowledgements.
          if (message.simulation.status === 'idle') {
            stopPendingRef.current = false;
            setStopPending(false);
          }
        } catch (error) {
          console.error('Error parsing JSON:', error);
        }
      };

      ws.onclose = () => {
        if (disposed) return;
        simulationRef.current = null;
        startPendingRef.current = false;
        stopPendingRef.current = false;
        setStartPending(false);
        setStopPending(false);
        setConnected(false);
        reconnectTimer = setTimeout(connect, 1000);
      };

      ws.onerror = () => ws.close();
    }

    connect();

    return () => {
      disposed = true;
      clearTimeout(reconnectTimer);
      wsRef.current?.close();
      wsRef.current = null;
    };
  }, []);

  useEffect(() => {
    window.localStorage.setItem('lb-dark-mode', String(darkMode));
  }, [darkMode]);

  function sendMessage(payload: Payload): boolean {
    const ws = wsRef.current;

    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.warn('WebSocket not connected. Cannot send message.');
      return false;
    }

    if (startPendingRef.current || stopPendingRef.current) {
      return false;
    }
    if (payload.Type === CommandType.StartSimulation) {
      if (!simulationRef.current || simulationRef.current.status === 'running') {
        return false;
      }
      // Guard immediately, before React renders the disabled Start button.
      startPendingRef.current = true;
      setStartPending(true);
    } else if (payload.Type === CommandType.StopSimulation) {
      if (!simulationRef.current || simulationRef.current.status === 'idle') {
        return false;
      }
      stopPendingRef.current = true;
      setStopPending(true);
    } else if (simulationRef.current?.status !== 'running') {
      return false;
    }
    setCommandError(null);
    try {
      ws.send(JSON.stringify(payload));
      return true;
    } catch (error) {
      startPendingRef.current = false;
      stopPendingRef.current = false;
      setStartPending(false);
      setStopPending(false);
      setCommandError('Failed to send the command. Check the server connection.');
      console.error('Cannot send WebSocket command:', error);
      return false;
    }
  }

  const topology = data?.topology && Array.isArray(data.topology.nodes) && Array.isArray(data.topology.links)
    ? data.topology
    : null;

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <Box sx={{ minHeight: '100vh', width: '100vw', bgcolor: 'background.default' }}>
        <AppBar position="static" color="primary" enableColorOnDark>
          <Toolbar sx={{ display: 'flex', justifyContent: 'space-between', gap: 2 }}>
            <Stack direction="row" spacing={2} alignItems="center">
              <FormControlLabel
                control={<Switch checked={darkMode} onChange={() => setDarkMode((prev) => !prev)} color="default" />}
                label="Dark mode"
                sx={{ color: 'inherit' }}
              />
              <Typography variant="h6" component="div">
                Load Balancer Simulator
              </Typography>
            </Stack>
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Chip
                label={connected ? 'Connected' : 'Disconnected'}
                color={connected ? 'success' : 'default'}
                variant={connected ? 'filled' : 'outlined'}
              />
              <HelpTooltip label="Connection status">
                Status of this page's connection to the simulator. The page reconnects automatically. Reconnecting or refreshing the page does not start a new experiment.
              </HelpTooltip>
            </Stack>
          </Toolbar>
        </AppBar>

        <Container maxWidth={false} sx={{ pb: 4 }}>
          <Tabs value={pathname.startsWith('/topology') ? '/topology' : '/'} aria-label="Main navigation" sx={{ borderBottom: 1, borderColor: 'divider', mt: 1 }}>
            <Tab component={RouterLink} to="/" value="/" label="Simulation" />
            <Tab component={RouterLink} to="/topology" value="/topology" label="Topology" />
          </Tabs>
          {/* Keep unsent settings when switching pages. */}
          <Box hidden={pathname !== '/'}>
            <ControlPanel
              key={connectionEpoch}
              connected={connected}
              simulation={data?.simulation ?? null}
              startPending={startPending}
              stopPending={stopPending}
              commandError={commandError}
              sendMessage={sendMessage}
            />
          </Box>

          <Suspense fallback={<Box sx={{ py: 4 }}><LinearProgress /><Typography sx={{ mt: 1 }}>Loading topology…</Typography></Box>}>
            <Routes>
              <Route path="/" element={<SimulationPage data={data} />} />
              <Route
                path="/topology"
                element={<TopologyPage topology={topology} simulation={data?.simulation ?? null} connected={connected} />}
              />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Suspense>
        </Container>
      </Box>
    </ThemeProvider>
  );
}

export default App;
