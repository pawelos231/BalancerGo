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
import type { LoadBalancerState } from './types';

export type Payload = {
  Payload: unknown;
  Type: string;
};

function App() {
  const [data, setData] = useState<LoadBalancerState | null>(null);
  const [connected, setConnected] = useState(false);
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
        if (!disposed) setConnected(true);
      };

      ws.onmessage = (event) => {
        if (disposed) return;
        try {
          const message = JSON.parse(event.data);
          setData(message);
        } catch (error) {
          console.error('Error parsing JSON:', error);
        }
      };

      ws.onclose = () => {
        if (disposed) return;
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

  function sendMessage(payload: Payload) {
    const ws = wsRef.current;

    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.warn('WebSocket not connected. Cannot send message.');
      return;
    }

    ws.send(JSON.stringify(payload));
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
                Load Balancer Status
              </Typography>
            </Stack>
            <Chip
              label={connected ? 'Connected' : 'Disconnected'}
              color={connected ? 'success' : 'default'}
              variant={connected ? 'filled' : 'outlined'}
            />
          </Toolbar>
        </AppBar>

        <Container maxWidth={false}>
          <ControlPanel connected={connected} sendMessage={sendMessage} />

          {data ? (
            <Stack spacing={3} sx={{ mt: 3 }}>
              <Card variant="outlined">
                <CardHeader title="Szczegoly LB" />
                <CardContent>
                  <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems={{ sm: 'center' }}>
                    <Chip label={`Mode: ${data.mode}`} color="info" variant="outlined" />
                    <Chip label={`Algorithm: ${data.algorithm}`} color="primary" variant="outlined" />
                  </Stack>
                </CardContent>
              </Card>

              <Card variant="outlined">
                <CardHeader title={`Serwery (${data.servers.length})`} />
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
                              action={<Chip label={active ? 'Active' : 'Inactive'} color={active ? 'success' : 'default'} size="small" />}
                            />
                            <CardContent sx={{ flexGrow: 1 }}>
                              <Stack spacing={1}>
                                <Typography variant="body2">Connections: {server.ActiveConnections}</Typography>
                                <Typography variant="body2">Half-Open: {server.HalfOpenConnections}</Typography>
                                <Typography variant="body2">P95 Latency: {server.LatencyP95Milli} ms</Typography>
                                <Typography variant="body2">Packets handled: {server.NumberOfHandledPackets}</Typography>
                                <Typography variant="body2">Cpu usage: {(server.CpuUsage * 100).toFixed(2)}%</Typography>
                                <Divider />
                                <Typography variant="subtitle2" gutterBottom>
                                  Clients ({clients.length})
                                </Typography>
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
              <Typography variant="body1">Waiting for data from WebSocket...</Typography>
            </Paper>
          )}
        </Container>
      </Box>
    </ThemeProvider>
  );
}

export default App;
