import {
  AppBar,
  Box,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Container,
  Divider,
  Grid,
  Paper,
  Stack,
  Toolbar,
  Typography,
  CssBaseline,
  Switch,
  FormControlLabel,
  createTheme,
  ThemeProvider,
} from '@mui/material';
import { useEffect, useRef, useState } from 'react';
import ControlPanel from './components/ControlPanel';
import HelpTooltip from './components/HelpTooltip';
import { CommandType } from './commands';
import type { CommandError, LoadBalancerState, SimulationConfig, SimulationMetadata } from './types';

export type Payload =
  | { Type: CommandType.SetAlgorithm; Payload: { Name: string } }
  | { Type: CommandType.SpawnClient; Payload: { Count: number } }
  | { Type: CommandType.StartSimulation; Payload: SimulationConfig }
  | { Type: CommandType.StopSimulation; Payload: Record<string, never> };

function App() {
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

        <Container maxWidth={false}>
          <ControlPanel
            key={connectionEpoch}
            connected={connected}
            simulation={data?.simulation ?? null}
            startPending={startPending}
            stopPending={stopPending}
            commandError={commandError}
            sendMessage={sendMessage}
          />

          {data ? (
            <Stack spacing={3} sx={{ mt: 3 }}>
              <Card variant="outlined">
                <CardHeader
                  title="Load balancer"
                  action={(
                    <HelpTooltip label="Network topology">
                      All clients share one link to the balancer and another for replies. Each backend has its own link in each direction. Every link has its own bandwidth limit and packet queue, shared by the TCP connections using it.
                    </HelpTooltip>
                  )}
                />
                <CardContent>
                  <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}>
                    <Chip label={`Algorithm: ${data.algorithm}`} color="primary" variant="outlined" />
                    <HelpTooltip label="Current algorithm">
                      The policy currently used to choose a backend for each new TCP connection. Established connections keep their assigned backend.
                    </HelpTooltip>
                  </Stack>
                </CardContent>
              </Card>

              <Card variant="outlined">
                <CardHeader title={`Backend servers (${data.servers.length})`} />
                <CardContent>
                  <Grid container spacing={2}>
                    {data.servers.map((serverData) => {
                      const { server, clients } = serverData;
                      const active = server.Active;
                      return (
                        <Grid size={{ xs: 12, sm: 6, md: 4 }} key={server.ID} sx={{ display: 'flex' }}>
                          <Card variant="outlined" sx={{ width: '100%', display: 'flex', flexDirection: 'column' }}>
                            <CardHeader
                              title={server.Name}
                              subheader={`${server.Address}:${server.Port}`}
                              action={(
                                <Stack direction="row" spacing={0.5} alignItems="center">
                                  <Chip label={active ? 'Active' : 'Inactive'} color={active ? 'success' : 'default'} size="small" />
                                  <HelpTooltip label="Backend activity">
                                    Whether this simulated server is enabled to handle packets. The balancer also checks health and connection capacity when choosing a backend.
                                  </HelpTooltip>
                                </Stack>
                              )}
                            />
                            <CardContent sx={{ flexGrow: 1 }}>
                              <Stack spacing={1}>
                                <Stack direction="row" spacing={0.5} alignItems="center">
                                  <Typography variant="body2">Established connections: {server.ActiveConnections}</Typography>
                                  <HelpTooltip label="Established connections">
                                    TCP connections whose handshake has completed at this server and that have not yet closed. Many connections can be open at the same virtual time.
                                  </HelpTooltip>
                                </Stack>
                                <Stack direction="row" spacing={0.5} alignItems="center">
                                  <Typography variant="body2">Half-open connections: {server.HalfOpenConnections}</Typography>
                                  <HelpTooltip label="Half-open connections">
                                    Connections for which the server received a SYN and is still waiting for the final handshake acknowledgement.
                                  </HelpTooltip>
                                </Stack>
                                <Stack direction="row" spacing={0.5} alignItems="center">
                                  <Typography variant="body2">P95 connection lifetime: {server.LatencyP95Milli} ms</Typography>
                                  <HelpTooltip label="P95 connection lifetime">
                                    95% of the last up to 100 connections completed with FIN had lifetimes at or below this value. Measured in virtual milliseconds from SYN arrival to FIN arrival at the server. Zero can mean there are no samples yet.
                                  </HelpTooltip>
                                </Stack>
                                <Stack direction="row" spacing={0.5} alignItems="center">
                                  <Typography variant="body2">Packets received: {server.NumberOfHandledPackets}</Typography>
                                  <HelpTooltip label="Packets received">
                                    All incoming TCP packets reaching this active server during the run, including handshake, data, closing packets and retransmissions. Includes packets the server later rejects.
                                  </HelpTooltip>
                                </Stack>
                                <Divider />
                                <Stack direction="row" spacing={0.5} alignItems="center">
                                  <Typography variant="subtitle2">Routed clients ({clients.length})</Typography>
                                  <HelpTooltip label="Routed clients">
                                    Clients currently assigned to this backend, including connections still completing their handshake. Entries disappear when the balancer removes the connection. Addresses and ports identify simulated endpoints.
                                  </HelpTooltip>
                                </Stack>
                                <Box
                                  sx={{
                                    maxHeight: 180,
                                    overflowY: 'auto',
                                    border: '1px solid',
                                    borderColor: 'divider',
                                    borderRadius: 1,
                                    p: 1,
                                    bgcolor: 'background.paper',
                                  }}
                                >
                                  <Stack spacing={1}>
                                    {clients.map((client, index) => (
                                      <Box
                                        key={`${server.ID}-${index}`}
                                        component="pre"
                                        sx={{
                                          m: 0,
                                          p: 1,
                                          bgcolor: 'action.hover',
                                          borderRadius: 1,
                                          fontSize: '0.85rem',
                                          whiteSpace: 'pre-wrap',
                                        }}
                                      >
                                        {JSON.stringify(client.Key, null, 2)}
                                      </Box>
                                    ))}
                                  </Stack>
                                </Box>
                              </Stack>
                            </CardContent>
                          </Card>
                        </Grid>
                      );
                    })}
                  </Grid>
                </CardContent>
              </Card>
            </Stack>
          ) : (
            <Paper variant="outlined" sx={{ mt: 3, p: 3, textAlign: 'center' }}>
              <Typography variant="body1">Waiting for simulation state…</Typography>
            </Paper>
          )}
        </Container>
      </Box>
    </ThemeProvider>
  );
}

export default App;
