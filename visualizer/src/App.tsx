import {
  Alert,
  Box,
  Button,
  Container,
  CssBaseline,
  LinearProgress,
  Tab,
  Tabs,
  ThemeProvider,
  Typography,
  createTheme,
} from '@mui/material';
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import {
  Link as RouterLink,
  Navigate,
  Route,
  Routes,
  useLocation,
} from 'react-router';
import AppHeader from './components/AppHeader';
import ControlPanel from './components/ControlPanel';
import ExperimentSetup from './components/ExperimentSetup';
import SimulationPerformance from './components/SimulationPerformance';
import useSimulation from './hooks/useSimulation';
import SimulationPage from './pages/SimulationPage';
import type { LoadBalancerState } from './types';
import type { Scenario } from './scenarios/types';
import useResults from './results/useResults';

const TopologyPage = lazy(() => import('./pages/TopologyPage'));
const PlannedTopologyPage = lazy(() => import('./pages/PlannedTopologyPage'));

function readDarkMode(): boolean {
  if (typeof window === 'undefined') return false;

  try {
    return window.localStorage.getItem('lb-dark-mode') === 'true';
  } catch {
    return false;
  }
}

function getTopology(data: LoadBalancerState | null) {
  const topology = data?.topology;

  if (!topology) return null;

  const hasNodes = Array.isArray(topology.nodes);
  const hasLinks = Array.isArray(topology.links);

  return hasNodes && hasLinks ? topology : null;
}

function App() {
  const { pathname } = useLocation();
  const {
    resultsSession,
    data,
    connected,
    startPending,
    stopPending,
    commandError,
    terminationUncertainty,
    currentRun,
    currentRunError,
    refreshCurrent,
    topologyLoading,
    topologyError,
    refreshTopology,
    sendMessage,
    stopAndSaveResults,
    startScenario,
  } = useSimulation();
  const results = useResults(resultsSession);
  const unsavedCount = results.state.candidates.length;
  const firstSaveError = Object.values(results.state.saveErrors)[0];
  const [darkMode, setDarkMode] = useState(readDarkMode);
  const [plannedScenario, setPlannedScenario] = useState<Scenario | null>(null);
  const [plannedError, setPlannedError] = useState<string | null>(null);

  const updatePlannedScenario = useCallback((scenario: Scenario | null, error: string | null) => {
    if (scenario) setPlannedScenario(scenario);
    setPlannedError(error);
  }, []);

  const simulation = data?.simulation ?? null;
  const topology = getTopology(data);
  const showSimulation = pathname === '/';
  const activeTab = pathname.startsWith('/planned-topology')
    ? '/planned-topology'
    : pathname.startsWith('/topology') ? '/topology' : '/';

  function retryUnsavedResults() {
    for (const candidate of results.state.candidates) results.history.retry(candidate.id);
  }

  const theme = useMemo(
    () => createTheme({
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
    }),
    [darkMode],
  );

  useEffect(() => {
    try {
      window.localStorage.setItem('lb-dark-mode', String(darkMode));
    } catch {
      // Keep the theme usable when browser storage is unavailable.
    }
  }, [darkMode]);

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />

      <Box
        sx={{
          minHeight: '100vh',
          width: '100vw',
          bgcolor: 'background.default',
        }}
      >
        <AppHeader
          connected={connected}
          darkMode={darkMode}
          onToggleDarkMode={() => setDarkMode((previous) => !previous)}
        />

        <Container maxWidth={false} sx={{ pb: 4 }}>
          <Tabs
            value={activeTab}
            aria-label="Main navigation"
            sx={{
              borderBottom: 1,
              borderColor: 'divider',
              mt: 1,
            }}
          >
            <Tab component={RouterLink} to="/" value="/" label="Simulation" />
            <Tab
              component={RouterLink}
              to="/planned-topology"
              value="/planned-topology"
              label="Planned topology"
            />
            <Tab
              component={RouterLink}
              to="/topology"
              value="/topology"
              label="Live topology"
            />
          </Tabs>

          {results.state.warning && (
            <Alert severity="warning" sx={{ mt: 2 }}>{results.state.warning}</Alert>
          )}
          {results.state.preparing.length > 0 && (
            <Alert severity="info" sx={{ mt: 2 }}>Preparing result metadata…</Alert>
          )}
          {results.state.storageError && (
            <Alert severity="warning" sx={{ mt: 2 }}>{results.state.storageError}</Alert>
          )}
          {unsavedCount > 0 && (
            <Alert
              severity="warning"
              sx={{ mt: 2 }}
              action={<Button color="inherit" onClick={retryUnsavedResults}>Retry save</Button>}
            >
              {unsavedCount} {unsavedCount === 1 ? 'result remains' : 'results remain'} in memory.
              {' '}Keep this tab open
              to retry saving or export them.
              {firstSaveError && <Box component="span" sx={{ display: 'block' }}>{firstSaveError}</Box>}
            </Alert>
          )}

          {activeTab !== '/planned-topology' && topologyLoading && (
            <Alert severity="info" sx={{ mt: 2 }}>
              Updating network topology. Controls remain available while it loads.
            </Alert>
          )}
          {terminationUncertainty && (
            <Alert severity="warning" sx={{ mt: 2 }}>
              {terminationUncertainty}
            </Alert>
          )}
          {activeTab !== '/planned-topology' && topologyError && (
            <Alert
              severity="error"
              sx={{ mt: 2 }}
              action={<Button color="inherit" onClick={refreshTopology}>Retry topology</Button>}
            >
              {topologyError}
            </Alert>
          )}

          {activeTab !== '/planned-topology' && (
            <SimulationPerformance performance={simulation?.performance} />
          )}

          {/* Keep unsent settings when switching pages. */}
          <Box hidden={!showSimulation}>
            <ControlPanel
              connected={connected}
              simulation={simulation}
              startPending={startPending}
              stopPending={stopPending}
              commandError={commandError}
              sendMessage={sendMessage}
              onStopAndSave={stopAndSaveResults}
            >
              <ExperimentSetup
                connected={connected}
                simulation={simulation}
                pending={startPending || stopPending}
                currentRun={currentRun}
                currentRunError={currentRunError}
                onRefreshCurrent={refreshCurrent}
                onStart={startScenario}
                onPreviewChange={updatePlannedScenario}
              />
            </ControlPanel>
          </Box>

          <Suspense
            fallback={
              <Box sx={{ py: 4 }}>
                <LinearProgress />
                <Typography sx={{ mt: 1 }}>Loading topology…</Typography>
              </Box>
            }
          >
            <Routes>
              <Route path="/" element={<SimulationPage data={data} />} />
              <Route
                path="/planned-topology"
                element={<PlannedTopologyPage scenario={plannedScenario} error={plannedError} />}
              />
              <Route
                path="/topology"
                element={
                  <TopologyPage
                    topology={topology}
                    simulation={simulation}
                    connected={connected}
                  />
                }
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
