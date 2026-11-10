import { useState } from 'react';
import {
  Accordion, AccordionDetails, AccordionSummary, Alert, Box, Card, CardContent,
  Chip, Stack, Table, TableBody, TableCell, TableContainer, TableHead,
  TablePagination, TableRow, Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { ExactTelemetry } from '../telemetry/exact';
import type { DurationSummary } from '../types';
import type { RunResult } from './types';
import {
  approximateValue, coverageNotes, exactValue, publicationRange, resultDate,
  resultListEntry, sourceExecutionError, virtualRange,
} from './presentation';
import ResultResources from './ResultResources';
import ResultCharts from './ResultCharts';

interface Metric {
  label: string;
  value: string;
}

function Metrics({ values }: { values: readonly Metric[] }) {
  return (
    <Box
      component="dl"
      sx={{
        display: 'grid',
        gridTemplateColumns: {
          xs: '1fr',
          sm: 'repeat(2, minmax(0, 1fr))',
          xl: 'repeat(3, minmax(0, 1fr))',
        },
        gap: 2,
        m: 0,
      }}
    >
      {values.map(metric => (
        <Box key={metric.label} sx={{ minWidth: 0 }}>
          <Typography component="dt" variant="body2" color="text.secondary">{metric.label}</Typography>
          <Typography
            component="dd"
            sx={{ m: 0, overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}
          >
            {metric.value}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" component="h3" sx={{ mb: 2 }}>{title}</Typography>
        {children}
      </CardContent>
    </Card>
  );
}

function PagedValues({ label, values }: { label: string; values: readonly string[] }) {
  const [page, setPage] = useState(0);
  const current = Math.min(page, Math.max(0, Math.ceil(values.length / 10) - 1));
  return (
    <Accordion disableGutters variant="outlined" sx={{ mt: 1 }}>
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Typography>{label} ({values.length})</Typography>
      </AccordionSummary>
      <AccordionDetails>
        <Box component="ul" sx={{ my: 0, pl: 3, overflowWrap: 'anywhere' }}>
          {values.slice(current * 10, current * 10 + 10).map((value, index) => (
            <li key={index}>{value}</li>
          ))}
        </Box>
        <TablePagination
          component="div" count={values.length} page={current} rowsPerPage={10}
          rowsPerPageOptions={[]} onPageChange={(_, next) => setPage(next)}
        />
      </AccordionDetails>
    </Accordion>
  );
}

function Coverage({ result }: { result: RunResult }) {
  const coverage = result.coverage;
  const notes = coverageNotes(coverage);
  return (
    <Section title="Observation coverage">
      <Alert severity={coverage.complete ? 'success' : 'warning'} sx={{ mb: 2 }}>
        {coverage.complete ? 'Complete observed publication history.' : 'Partial observed publication history.'}
        {' '}Only received samples are retained; missing intervals are not reconstructed.
      </Alert>
      <Metrics values={[
        { label: 'Retained samples', value: exactValue(result.collection.samples.length) },
        { label: 'First observed publication', value: exactValue(result.collection.firstObservedSequence) },
        { label: 'Highest observed publication', value: exactValue(result.collection.highestObservedSequence) },
        { label: 'Terminal publication', value: exactValue(result.collection.terminalPublicationSequence) },
        { label: 'Topology definitions retained', value: exactValue(result.collection.definitions.length) },
        { label: 'Unrecorded publications', value: exactValue(coverage.unrecordedPublicationCount) },
      ]} />
      {notes.length > 0 && (
        <Box component="ul" sx={{ mt: 2, mb: 0, pl: 3 }}>
          {notes.map(note => <li key={note}>{note}</li>)}
        </Box>
      )}
      {coverage.unrecordedPublications.length > 0 && (
        <PagedValues
          label="Missing publication ranges"
          values={coverage.unrecordedPublications.map(publicationRange)}
        />
      )}
      {coverage.missingDefinitionRevisions.length > 0 && (
        <PagedValues label="Missing definition revision IDs" values={coverage.missingDefinitionRevisions} />
      )}
    </Section>
  );
}

function TransferDurations({ values }: {
  values: readonly { label: string; summary: ExactTelemetry<DurationSummary> }[];
}) {
  return (
    <TableContainer sx={{ mt: 2 }}>
      <Table size="small" aria-label="Transfer duration summaries">
        <TableHead>
          <TableRow>
            <TableCell>Duration (virtual s)</TableCell>
            <TableCell>Samples</TableCell>
            <TableCell>Mean</TableCell>
            <TableCell>Minimum</TableCell>
            <TableCell>Maximum</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {values.map(({ label, summary }) => (
            <TableRow key={label}>
              <TableCell component="th" scope="row">{label}</TableCell>
              <TableCell>{exactValue(summary.samples)}</TableCell>
              {(['meanSeconds', 'minSeconds', 'maxSeconds'] as const).map(key => (
                <TableCell key={key}>
                  {String(summary.samples) === '0' ? 'Unavailable' : approximateValue(summary[key])}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function Summary({ result }: { result: RunResult }) {
  const { payload, transfers, performance, payloadRate, observation, basis } = result.summary;
  const failureCounts = transfers ? Object.entries(transfers.failures) : [];
  return (
    <Stack spacing={2}>
      <Section title="Payload and transfers">
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {basis === 'terminal' ? 'Cumulative counters from the terminal publication.'
            : basis === 'lastObservation' ? 'Cumulative counters from the last retained observation.'
              : 'No summary sample is available.'}
          {observation && (
            <>
              {' '}Publication {observation.publicationSequence},
              {' '}{approximateValue(observation.virtualTimeSeconds, 'virtual s')}.
            </>
          )}
          {' '}Counters are not added across samples. Decimal counter digits are preserved; ≈ marks approximate displays.
        </Typography>
        <Metrics values={[
          { label: 'Received payload (B)', value: exactValue(payload?.receivedBytes) },
          { label: 'Acknowledged payload (B)', value: exactValue(payload?.acknowledgedBytes) },
          {
            label: 'Received payload / virtual s',
            value: approximateValue(payloadRate?.approximateReceivedBytesPerSecond, 'B / virtual s'),
          },
          {
            label: 'Acknowledged goodput / virtual s',
            value: approximateValue(payloadRate?.approximateAcknowledgedBytesPerSecond, 'B / virtual s'),
          },
          { label: 'Transfers started', value: exactValue(transfers?.started) },
          { label: 'Transfers not started', value: exactValue(transfers?.notStarted) },
          { label: 'Transfers succeeded', value: exactValue(transfers?.succeeded) },
          { label: 'Transfers failed', value: exactValue(transfers?.failed) },
          { label: 'Transfers in progress', value: exactValue(transfers?.inProgress) },
          { label: 'SYN retransmissions', value: exactValue(transfers?.synRetransmissions) },
          { label: 'Data retransmissions', value: exactValue(transfers?.dataRetransmissions) },
        ]} />
        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          Payload rates divide cumulative bytes by the run's virtual time, including time before this browser joined.
          Global transfer latency p95 / p99: unavailable in the received contract.
        </Typography>
        {failureCounts.length > 0 && (
          <Box sx={{ mt: 2 }}>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>Transfer failure reasons</Typography>
            <Metrics values={failureCounts.map(([reason, count]) => ({
              label: reason.replace(/([a-z])([A-Z])/g, '$1 $2'), value: exactValue(count),
            }))} />
          </Box>
        )}
        {transfers && (
          <TransferDurations values={[
            { label: 'Handshake', summary: transfers.handshake },
            { label: 'Payload', summary: transfers.payload },
            { label: 'Completion', summary: transfers.completion },
          ]} />
        )}
      </Section>
      <Section title="Engine performance (real time)">
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Backend execution averages include pacing and snapshot work. These rates measure simulator work per real second.
        </Typography>
        <Metrics values={[
          { label: 'Elapsed real execution time', value: approximateValue(performance?.wallTimeSeconds, 's') },
          { label: 'Events executed', value: exactValue(performance?.eventsExecuted) },
          { label: 'Events / real s', value: approximateValue(performance?.eventsPerWallSecond, 'events / real s') },
          { label: 'LB packets forwarded', value: exactValue(performance?.forwardedPackets) },
          { label: 'LB packets / real s', value: approximateValue(performance?.forwardedPacketsPerWallSecond, 'packets / real s') },
        ]} />
      </Section>
    </Stack>
  );
}

function Context({ result }: { result: RunResult }) {
  const [scenarioExpanded, setScenarioExpanded] = useState(false);
  const { context } = result;
  const experiment = context.experiment;
  const row = resultListEntry(result);
  const interval = context.publicationInterval.configuredNanoseconds;
  const intervalDisplay = interval === null ? 'Unavailable'
    : `${exactValue(interval)} ns (${approximateValue(interval, 'ms', 1e-6)})`;
  return (
    <Section title="Run and experiment context">
      <Metrics values={[
        { label: 'Local result ID', value: result.id },
        { label: 'Run ID', value: result.runId },
        { label: 'Result format version', value: exactValue(result.resultVersion) },
        { label: 'Completion received', value: resultDate(result.completion.receivedAt) },
        { label: 'First observation received', value: resultDate(result.collection.firstObservedAt) },
        { label: 'Observed virtual time range', value: virtualRange(row.virtualTimeStartSeconds, row.virtualTimeEndSeconds) },
        { label: 'Seed', value: exactValue(context.seed) },
        { label: 'Summary algorithm', value: exactValue(row.algorithm) },
        { label: 'Model version', value: exactValue(experiment?.model) },
        { label: 'Scenario SHA-256', value: exactValue(experiment?.scenarioSha256) },
        { label: 'Scenario provenance', value: context.scenarioProvenance },
        { label: 'Scenario metadata received', value: resultDate(context.scenarioReceivedAt) },
        { label: 'Experiment provenance', value: context.experimentProvenance },
        { label: 'Experiment metadata received', value: resultDate(context.experimentReceivedAt) },
        { label: 'Configured publication interval (real time)', value: intervalDisplay },
        { label: 'Execution speed', value: approximateValue(experiment?.execution.speed) },
        { label: 'Headless execution', value: exactValue(experiment?.execution.headless) },
        { label: 'Trace enabled', value: exactValue(experiment?.execution.traceEnabled) },
        { label: 'Go version', value: exactValue(experiment?.build.goVersion) },
        { label: 'Module version', value: exactValue(experiment?.build.moduleVersion) },
        {
          label: 'Build OS / architecture',
          value: experiment ? `${experiment.build.goos} / ${experiment.build.goarch}` : 'Unavailable',
        },
        { label: 'VCS revision', value: exactValue(experiment?.build.vcsRevision) },
        { label: 'VCS modified', value: exactValue(experiment?.build.vcsModified) },
      ]} />
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
        Publication scheduling uses real time. Command and terminal publications can arrive between scheduled intervals.
      </Typography>
      {context.scenario && (
        <Accordion
          disableGutters variant="outlined" expanded={scenarioExpanded}
          onChange={(_, expanded) => setScenarioExpanded(expanded)} sx={{ mt: 2 }}
        >
          <AccordionSummary expandIcon={<ExpandMoreIcon />}>
            <Typography>Executed scenario JSON</Typography>
          </AccordionSummary>
          <AccordionDetails>
            {scenarioExpanded && (
              <Box
                component="pre"
                sx={{
                  m: 0,
                  p: 1,
                  maxHeight: 420,
                  overflow: 'auto',
                  fontSize: 12,
                  whiteSpace: 'pre-wrap',
                  overflowWrap: 'anywhere',
                }}
              >
                {JSON.stringify(context.scenario, null, 2)}
              </Box>
            )}
          </AccordionDetails>
        </Accordion>
      )}
    </Section>
  );
}

export default function ResultDetails({ result }: { result: RunResult }) {
  const executionError = sourceExecutionError(result);
  return (
    <Stack spacing={2}>
      <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
        <Typography variant="h5" component="h2" sx={{ overflowWrap: 'anywhere' }}>Run {result.runId}</Typography>
        <Chip label={result.completion.reason === 'finished' ? 'Finished' : 'Stopped'} size="small" />
      </Stack>
      {executionError && <Alert severity="error">Source execution error: {executionError}</Alert>}
      <Summary result={result} />
      <Coverage result={result} />
      <ResultCharts result={result} />
      <ResultResources result={result} />
      <Context result={result} />
    </Stack>
  );
}
