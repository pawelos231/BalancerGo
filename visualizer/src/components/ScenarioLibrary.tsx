import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Grid,
  Stack,
  Typography,
} from '@mui/material';
import { scenarioPresets } from '../scenarios/presets';
import type { Scenario, ScenarioSummary } from '../scenarios/types';

// A chosen candidate stays separate from the scenario currently shown in the form.
export interface PresetCandidate {
  id: string;
  title: string;
  scenario: Scenario;
}

// Manual edits keep the loaded preset's name but invalidate its validation badge.
export interface AppliedPreset {
  id: string;
  title: string;
  summary: ScenarioSummary;
  pristine: boolean;
}

interface ScenarioLibraryProps {
  defaultsReady: boolean;
  loading: boolean;
  pending: boolean;
  validating: boolean;
  candidate: PresetCandidate | null;
  applied: AppliedPreset | null;
  error: string | null;
  onChoose: (id: string) => void;
  onValidate: () => void;
  onRetryDefaults: () => void;
}

// Validation loads a preset into the form; the form owns the single Start action.
function ScenarioLibrary({
  defaultsReady,
  loading,
  pending,
  validating,
  candidate,
  applied,
  error,
  onChoose,
  onValidate,
  onRetryDefaults,
}: ScenarioLibraryProps) {
  const canChoose = defaultsReady && !validating && !pending;
  const canValidate = candidate !== null && !validating && !pending;

  return (
    <Card variant="outlined">
      <CardHeader
        title="Scenario library"
        subheader="Choose a preset and validate it to load its settings into the form below."
      />
      <CardContent>
        <Stack spacing={2}>
          <Typography variant="body2" color="text.secondary">
            Each preset runs for 180 virtual seconds at speed 1, with 1 GiB per client.
            Other settings start from backend defaults. Choosing a preset does not change
            the form or start a run.
          </Typography>

          {loading && (
            <Typography variant="body2">Loading backend scenario defaults…</Typography>
          )}
          {error && <Alert severity="error">{error}</Alert>}
          {!defaultsReady && !loading && (
            <Button variant="outlined" onClick={onRetryDefaults} sx={{ alignSelf: 'flex-start' }}>
              Retry loading defaults
            </Button>
          )}

          <Grid container spacing={2}>
            {scenarioPresets.map((preset) => {
              const selected = candidate?.id === preset.id;
              return (
                <Grid key={preset.id} size={{ xs: 12, md: 6 }} sx={{ display: 'flex' }}>
                  <Card
                    variant="outlined"
                    sx={{
                      width: '100%',
                      borderColor: selected ? 'primary.main' : 'divider',
                    }}
                  >
                    <CardContent>
                      <Stack spacing={1} alignItems="flex-start">
                        <Typography variant="subtitle1">{preset.title}</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {preset.description}
                        </Typography>
                        <Button
                          variant={selected ? 'contained' : 'outlined'}
                          disabled={!canChoose || selected}
                          aria-label={`Choose ${preset.title}`}
                          onClick={() => onChoose(preset.id)}
                        >
                          {selected ? 'Chosen' : 'Choose'}
                        </Button>
                      </Stack>
                    </CardContent>
                  </Card>
                </Grid>
              );
            })}
          </Grid>

          {candidate && (
            <Stack spacing={1} alignItems="flex-start">
              <Typography variant="subtitle2">Selected candidate: {candidate.title}</Typography>
              <Button variant="outlined" disabled={!canValidate} onClick={onValidate}>
                {validating ? 'Validating…' : 'Validate and load preset'}
              </Button>
            </Stack>
          )}

          {applied && (
            <Stack spacing={1}>
              <Typography variant="subtitle2">Loaded into form: {applied.title}</Typography>
              {applied.pristine ? (
                <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                  <Chip label="Applied and validated" color="success" size="small" />
                  <Chip label={`${applied.summary.totalClients} clients`} size="small" />
                  <Chip label={`${applied.summary.backendCount} backends`} size="small" />
                  <Chip label={`${applied.summary.clientGroupCount} groups`} size="small" />
                </Stack>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  Draft edited after loading. Start simulation validates the current settings again.
                </Typography>
              )}
            </Stack>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}

export default ScenarioLibrary;
