import {
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Divider,
  FormControl,
  FormControlLabel,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import { useMemo, useState } from 'react';
import type { LBMode } from '../types';
import type { Payload } from '../App';

interface ControlPanelProps {
  connected: boolean;
  sendMessage: (payload: Payload) => void;
}

const modes: LBMode[] = ['NAT', 'SNAT', 'DSR'];

function ControlPanel({ connected, sendMessage }: ControlPanelProps) {
  const [algorithm, setAlgorithm] = useState('roundRobin');
  const [delayMs, setDelayMs] = useState('150');
  const [mode, setMode] = useState<LBMode>('NAT');
  // const [clientId, setClientId] = useState('client-1');
  const [serverId, setServerId] = useState('server-1');
  const [clientCount, setClientCount] = useState('1');
  const [serverAddress, setServerAddress] = useState('127.0.0.1');
  const [serverPort, setServerPort] = useState('8081');
  const [protocol, setProtocol] = useState('TCP');
  const [serverParams, setServerParams] = useState('{"maxConn": 100, "weight": 1}');
  const [synCookiesEnabled, setSynCookiesEnabled] = useState(true);

  const disabled = useMemo(() => !connected, [connected]);

  const send = (payload: Payload) => {
    sendMessage(payload);
  };

  const parseNumber = (value: string, fallback = 0) => {
    const numeric = Number(value);
    return Number.isFinite(numeric) ? numeric : fallback;
  };

  return (
    <Box sx={{ width: '100%' }}>
      <Grid container spacing={2} alignItems="stretch" sx={{ width: '100%', padding: 2 }}>
        {/* Routowanie */}
        <Grid size={{ xs: 12, sm: 6, md: 3 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
            <CardHeader title="Routowanie" subheader="Szybkie przelaczanie algorytmu i trybu pracy." />
            <CardContent sx={{ flexGrow: 1 }}>
              <Stack spacing={2}>
                <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems="center">
                  <FormControl fullWidth size="small">
                    <InputLabel id="alg-label">Algorytm</InputLabel>
                    <Select labelId="alg-label" label="Algorytm" value={algorithm} onChange={(event) => setAlgorithm(event.target.value)}>
                      <MenuItem value="roundRobin">roundRobin</MenuItem>
                      <MenuItem value="random">random</MenuItem>
                    </Select>
                  </FormControl>
                  <Button
                    variant="contained"
                    disabled={disabled}
                    onClick={() => send({ Type: 'setAlgorithm', Payload: { Name: algorithm } })}
                  >
                    Wyslij
                  </Button>
                </Stack>

                <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems="center">
                  <FormControl fullWidth size="small">
                    <InputLabel id="mode-label">Tryb LB</InputLabel>
                    <Select
                      disabled
                      labelId="mode-label"
                      label="Tryb LB"
                      value={mode}
                      onChange={(event) => setMode(event.target.value as LBMode)}
                    >
                      {modes.map((modeOption) => (
                        <MenuItem key={modeOption} value={modeOption}>
                          {modeOption}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                  <Button variant="outlined" disabled={disabled} onClick={() => send({ Type: 'changeMode', Payload: { mode } })}>
                    Zmien tryb
                  </Button>
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Klienci */}
        <Grid size={{ xs: 12, sm: 6, md: 3 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
            <CardHeader title="Klienci" subheader="Spawn, protokol, SYN flood, disconnect/kill." />
            <CardContent sx={{ flexGrow: 1 }}>
              <Stack spacing={2}>
                {/* <TextField size="small" label="Id klienta" value={clientId} onChange={(e) => setClientId(e.target.value)} /> */}
                <TextField
                  size="small"
                  label="Liczba klientow"
                  value={clientCount}
                  onChange={(e) => setClientCount(e.target.value)}
                  inputProps={{ inputMode: 'numeric', pattern: '[0-9]*' }}
                />
                <Stack direction="row" spacing={2} flexWrap="wrap" justifyContent="space-between">
                  <Button
                    sx={{ flex: 1 }}
                    variant="contained"
                    disabled={disabled}
                    onClick={() =>
                      send({
                        Type: 'spawnClient',
                        Payload: {
                          Count: parseNumber(clientCount, 1),
                        },
                      })
                    }
                  >
                    Spawn klienta
                  </Button>

                  <Button
                    sx={{ flex: 1 }}
                    variant="outlined"
                    disabled={disabled}
                    onClick={() => send({ Type: 'synFloodByClient', Payload: {} })}
                  >
                    SYN flood
                  </Button>

                  <Button
                    sx={{ flex: 1 }}
                    variant="outlined"
                    color="warning"
                    disabled={disabled}
                    onClick={() => send({ Type: 'disconnectClient', Payload: {} })}
                  >
                    Rozłącz
                  </Button>

                  <Button
                    sx={{ flex: 1 }}
                    variant="outlined"
                    color="error"
                    disabled={disabled}
                    onClick={() => send({ Type: 'killClient', Payload: {} })}
                  >
                    Kill
                  </Button>
                </Stack>

                <Divider />
                <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems="center">
                  <FormControl fullWidth size="small">
                    <InputLabel id="protocol-label">Protokol klienta</InputLabel>
                    <Select
                      labelId="protocol-label"
                      label="Protokol klienta"
                      value={protocol}
                      onChange={(event) => setProtocol(event.target.value)}
                    >
                      <MenuItem value="TCP">TCP</MenuItem>
                      <MenuItem value="UDP">UDP</MenuItem>
                      <MenuItem value="HTTP">HTTP</MenuItem>
                    </Select>
                  </FormControl>
                  <Button
                    variant="outlined"
                    disabled={disabled}
                    onClick={() => send({ Type: 'changeProtocolForClient', Payload: { protocol } })}
                  >
                    Zmien protokol
                  </Button>
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Backend-y */}
        <Grid size={{ xs: 12, sm: 6, md: 3 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
            <CardHeader title="Backendy" subheader="Spawn, rozlaczenie, kill, parametry." />
            <CardContent sx={{ flexGrow: 1 }}>
              <Stack spacing={2}>
                <TextField size="small" label="Id backendu" value={serverId} onChange={(e) => setServerId(e.target.value)} />
                <TextField size="small" label="Adres" value={serverAddress} onChange={(e) => setServerAddress(e.target.value)} />
                <TextField
                  size="small"
                  label="Port"
                  value={serverPort}
                  onChange={(e) => setServerPort(e.target.value)}
                  inputProps={{ inputMode: 'numeric', pattern: '[0-9]*' }}
                />
                <Stack direction="row" spacing={1} flexWrap="wrap">
                  <Button
                    variant="contained"
                    disabled={disabled}
                    onClick={() =>
                      send({
                        Type: 'spawnServer',
                        Payload: {
                          serverId,
                          address: serverAddress,
                          port: parseNumber(serverPort, 0),
                        },
                      })
                    }
                  >
                    Spawn backend
                  </Button>
                  <Button
                    variant="outlined"
                    color="warning"
                    disabled={disabled}
                    onClick={() => send({ Type: 'disconnectBackend', Payload: { serverId } })}
                  >
                    Rozlacz
                  </Button>
                  <Button
                    variant="outlined"
                    color="error"
                    disabled={disabled}
                    onClick={() => send({ Type: 'killBackend', Payload: { serverId } })}
                  >
                    Kill
                  </Button>
                </Stack>
                <TextField
                  label="Parametry backendu (JSON)"
                  multiline
                  minRows={3}
                  maxRows={6}
                  value={serverParams}
                  onChange={(e) => setServerParams(e.target.value)}
                />
                <Button
                  variant="outlined"
                  disabled={disabled}
                  onClick={() => {
                    let parsed: unknown = serverParams;
                    try {
                      parsed = JSON.parse(serverParams);
                    } catch {
                      // leave as string if parsing fails
                    }
                    send({ Type: 'changeServerParameters', Payload: { serverId, params: parsed } });
                  }}
                >
                  Zmien parametry
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Symulacje */}
        <Grid size={{ xs: 12, sm: 6, md: 3 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
            <CardHeader title="Symulacje" subheader="Opoznienia i SYN cookies." />
            <CardContent sx={{ flexGrow: 1 }}>
              <Stack spacing={2}>
                <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} alignItems="center">
                  <TextField
                    size="small"
                    label="Opoznienie (ms)"
                    value={delayMs}
                    onChange={(e) => setDelayMs(e.target.value)}
                    inputProps={{ inputMode: 'numeric', pattern: '[0-9]*' }}
                  />
                  <Button
                    variant="contained"
                    disabled={disabled}
                    onClick={() => send({ Type: 'changeDelay', Payload: { delayMs: parseNumber(delayMs, 0) } })}
                  >
                    Ustaw opoznienie
                  </Button>
                </Stack>
                <Divider />
                <Stack direction="row" spacing={1} alignItems="center" justifyContent="space-between">
                  <Box>
                    <Typography variant="subtitle2">SYN cookies</Typography>
                    <Typography variant="body2" color="text.secondary">
                      Wlacz/ wylacz mechanizm.
                    </Typography>
                  </Box>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={synCookiesEnabled}
                        onChange={() => {
                          const newValue = !synCookiesEnabled;
                          setSynCookiesEnabled(newValue);
                          send({ Type: 'switchSynCookies', Payload: { enabled: newValue } });
                        }}
                        color="primary"
                        disabled={disabled}
                      />
                    }
                    label={synCookiesEnabled ? 'ON' : 'OFF'}
                  />
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

export default ControlPanel;
