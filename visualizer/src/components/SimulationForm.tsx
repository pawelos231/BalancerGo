import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Stack,
  Typography,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { FormEvent } from 'react';
import type { ScenarioDraft, ScenarioField } from '../scenarios/draft';
import { scenarioFieldSections } from '../scenarios/formFields';
import type { Scenario } from '../scenarios/types';
import ScenarioFieldInput from './ScenarioFieldInput';

interface SimulationFormProps {
  scenario: Scenario;
  values: ScenarioDraft;
  errors: Partial<Record<ScenarioField, string>>;
  connected: boolean;
  running: boolean;
  pending: boolean;
  validating: boolean;
  onChange: (name: ScenarioField, raw: string) => void;
  onFocus: (name: ScenarioField) => void;
  onBlur: (name: ScenarioField) => void;
  onStart: () => void;
}

// Sections share one scenario draft and one Start action.
function SimulationForm({
  scenario,
  values,
  errors,
  connected,
  running,
  pending,
  validating,
  onChange,
  onFocus,
  onBlur,
  onStart,
}: SimulationFormProps) {
  const hasErrors = Object.keys(errors).length > 0;
  const canStart = connected && !running && !pending && !validating && !hasErrors;
  const fieldsDisabled = !connected || pending || validating;
  const groups = scenario.workload.clientGroups;
  const displayedGroups = groups.slice(0, 12);
  const profiles = scenario.network.profiles ?? [];

  const startLabel = pending ? 'Waiting for confirmation…' : 'Start simulation';
  const nextRunHint = running
    ? 'You can prepare the parameters for the next run.'
    : 'Start uses the settings in all sections.';

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (canStart) {
      onStart();
    }
  }

  return (
    <Card component="form" variant="outlined" onSubmit={submit} noValidate>
      <CardHeader
        title="Start simulation"
        subheader="Configure the experiment, TCP traffic, resources and links separately."
      />

      <CardContent>
        <Stack spacing={2}>
          <Alert severity="info">
            {groups.length} client groups and {profiles.length} link profiles are retained.
            Traffic and link fields edit defaults; group, profile and client overrides stay in place.
          </Alert>

          {scenarioFieldSections.map((section) => {
            const errorCount = section.fields.filter((name) => Boolean(errors[name])).length;
            const summaryID = `settings-${section.id}-summary`;
            const panelID = `settings-${section.id}-panel`;
            const isTrafficSection = section.id === 'tcp-traffic';

            return (
              <Accordion
                key={section.id}
                defaultExpanded={section.defaultExpanded}
                disableGutters
                elevation={0}
                sx={{ border: '1px solid', borderColor: 'divider' }}
              >
                <AccordionSummary
                  id={summaryID}
                  aria-controls={panelID}
                  expandIcon={<ExpandMoreIcon />}
                >
                  <Stack spacing={0.5} sx={{ pr: 2 }}>
                    <Stack direction="row" spacing={1} alignItems="center">
                      <Typography component="span" variant="subtitle1">
                        {section.title}
                      </Typography>

                      {errorCount > 0 && (
                        <Chip label={`${errorCount} invalid`} color="error" size="small" />
                      )}
                    </Stack>

                    <Typography variant="body2" color="text.secondary">
                      {section.description}
                    </Typography>
                  </Stack>
                </AccordionSummary>

                <AccordionDetails>
                  <Stack spacing={2}>
                    {isTrafficSection && (
                      <Stack spacing={1}>
                        <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                          {displayedGroups.map((group) => (
                            <Chip
                              key={group.id}
                              label={`${group.id}: ${group.count} clients`}
                              size="small"
                            />
                          ))}
                        </Stack>

                        {displayedGroups.length < groups.length && (
                          <Typography variant="caption" color="text.secondary">
                            {groups.length - displayedGroups.length} more groups are retained in the scenario.
                          </Typography>
                        )}

                        {groups.length > 1 && (
                          <Typography variant="body2" color="text.secondary">
                            Changing the total redistributes clients proportionally across these groups.
                          </Typography>
                        )}
                      </Stack>
                    )}

                    <Grid container spacing={2}>
                      {section.fields.map((name) => (
                        <Grid size={{ xs: 12, sm: 6, md: 4 }} key={name}>
                          <ScenarioFieldInput
                            name={name}
                            value={values[name]}
                            error={errors[name]}
                            disabled={fieldsDisabled}
                            onChange={onChange}
                            onFocus={onFocus}
                            onBlur={onBlur}
                          />
                        </Grid>
                      ))}
                    </Grid>
                  </Stack>
                </AccordionDetails>
              </Accordion>
            );
          })}

          {hasErrors && (
            <Alert severity="error">
              Correct the highlighted settings before starting.
              Sections with invalid settings show an error badge, including when collapsed.
            </Alert>
          )}

          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={2}
            alignItems={{ sm: 'center' }}
          >
            <Button type="submit" variant="contained" disabled={!canStart}>
              {startLabel}
            </Button>

            <Typography variant="body2" color="text.secondary">
              {nextRunHint}
            </Typography>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

export default SimulationForm;
