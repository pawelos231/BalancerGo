import {
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  TextField,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import { useState } from 'react';
import type { Payload } from '../App';

interface ControlPanelProps {
  connected: boolean;
  sendMessage: (payload: Payload) => void;
}

function ControlPanel({ connected, sendMessage }: ControlPanelProps) {
  const [algorithm, setAlgorithm] = useState('roundRobin');
  const [clientCount, setClientCount] = useState('1');

  const count = Number(clientCount);
  const validCount = Number.isInteger(count) && count >= 1 && count <= 10000;

  return (
    <Box sx={{ width: '100%' }}>
      <Grid container spacing={2} sx={{ width: '100%', padding: 2 }}>
        <Grid size={{ xs: 12, sm: 6 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardHeader title="Algorytm balancera" />
            <CardContent>
              <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} alignItems="center">
                <FormControl fullWidth size="small">
                  <InputLabel id="alg-label">Algorytm</InputLabel>
                  <Select
                    labelId="alg-label"
                    label="Algorytm"
                    value={algorithm}
                    onChange={(event) => setAlgorithm(event.target.value)}
                  >
                    <MenuItem value="roundRobin">Round Robin</MenuItem>
                    <MenuItem value="random">Random</MenuItem>
                  </Select>
                </FormControl>
                <Button
                  variant="contained"
                  disabled={!connected}
                  onClick={() => sendMessage({ Type: 'setAlgorithm', Payload: { Name: algorithm } })}
                >
                  Zastosuj
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, sm: 6 }} display="flex">
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardHeader title="Klienci TCP" />
            <CardContent>
              <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} alignItems="flex-start">
                <TextField
                  fullWidth
                  size="small"
                  type="number"
                  label="Liczba klientów"
                  value={clientCount}
                  onChange={(event) => setClientCount(event.target.value)}
                  inputProps={{ min: 1, max: 10000, step: 1 }}
                  error={!validCount}
                  helperText={!validCount ? 'Podaj liczbę całkowitą od 1 do 10000.' : undefined}
                />
                <Button
                  variant="contained"
                  disabled={!connected || !validCount}
                  onClick={() => sendMessage({ Type: 'spawnClient', Payload: { Count: count } })}
                >
                  Dodaj klientów
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

export default ControlPanel;
