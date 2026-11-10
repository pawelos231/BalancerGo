import { useMemo, useState } from 'react';
import {
  Accordion, AccordionDetails, AccordionSummary, Box, Button, Chip, Stack,
  Table, TableBody, TableCell, TableContainer, TableHead, TablePagination,
  TableRow, Tab, Tabs, Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { ExactTelemetry } from '../telemetry/exact';
import type { TopologyDefinition } from '../telemetry/types';
import type { LastObservation, RunResult } from './types';
import { approximateValue, clientTransferDurationValue, exactValue, resultDate } from './presentation';

type ResourceKind = 'nodes' | 'servers' | 'clients' | 'links' | 'queues' | 'ingress';
type Resource = LastObservation<unknown>;
type Definition = ExactTelemetry<TopologyDefinition>;

const groups: readonly { kind: ResourceKind; label: string }[] = [
  { kind: 'nodes', label: 'Nodes / hosts' },
  { kind: 'servers', label: 'Backends' },
  { kind: 'clients', label: 'Clients' },
  { kind: 'links', label: 'Links' },
  { kind: 'queues', label: 'Ingress queues' },
  { kind: 'ingress', label: 'Shared ingress' },
];

function readable(key: string): string {
  return key.replace(/([a-z\d])([A-Z])/g, '$1 $2').replace(/^./, letter => letter.toUpperCase());
}

function metricValue(key: string, value: unknown, unit?: string): string {
  if (typeof value !== 'number' && typeof value !== 'string' && typeof value !== 'boolean') {
    return 'Unavailable';
  }
  if (typeof value === 'boolean' || typeof value === 'string' && !/^-?\d/.test(value)) {
    return exactValue(value);
  }
  if (/hostUtilization|cpuUsage/.test(key)) return approximateValue(value, '%', 100);
  if (/Seconds$|Milli$|PerSecond$/.test(key) || typeof value === 'number' && !Number.isInteger(value)) {
    const fallback = key.endsWith('BytesPerSecond') ? 'B / virtual s'
      : key.endsWith('Milli') ? 'ms' : key.endsWith('Seconds') ? 's' : '';
    return approximateValue(value, unit ?? fallback);
  }
  return `${exactValue(value)}${unit ? ` ${unit}` : ''}`;
}

function resourceLabel(kind: ResourceKind, row: Resource, definition?: Definition): string {
  if (kind === 'nodes') return definition?.nodes.find(node => node.id === row.id)?.label ?? row.id;
  if (kind === 'servers') return definition?.servers.find(server => server.id === row.id)?.name ?? row.id;
  if (kind === 'links') return definition?.links.find(link => link.id === row.id)?.label ?? row.id;
  if (kind === 'queues') return definition?.ingress?.queues.find(queue => queue.id === row.id)?.label ?? row.id;
  return kind === 'ingress' ? 'Shared ingress' : row.id;
}

function ResourceMetrics({ row, kind, definition }: {
  row: Resource;
  kind: ResourceKind;
  definition?: Definition;
}) {
  const [page, setPage] = useState(0);
  const fields: { key: string; label: string; value: string }[] = [];
  const node = kind === 'nodes' ? definition?.nodes.find(value => value.id === row.id) : undefined;

  function collect(value: unknown, path = '') {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) return;
    for (const [key, child] of Object.entries(value)) {
      if (key === 'id' || key === 'clients') continue;
      const next = path ? `${path}.${key}` : key;
      if (child !== null && typeof child === 'object') {
        collect(child, next);
        continue;
      }
      const metric = path === 'metrics' ? node?.metrics.find(metric => metric.key === key) : undefined;
      const label = key === 'latencyP95Milli' ? 'Backend connection lifetime p95'
        : key === 'hostUtilization' || key === 'cpuUsage' ? 'CPU utilization (host lifetime mean)'
          : key === 'hostAvailableCoreTimeSeconds' ? 'Available core time (host lifetime)'
            : metric?.label ?? readable(next.replace(/\./g, ' / '));
      const unit = metric?.unit ?? (key === 'hostAvailableCoreTimeSeconds' ? 'core s'
        : key.endsWith('Nanoseconds') ? 'ns' : key.endsWith('Bytes') ? 'B' : undefined);
      const display = kind === 'clients' && path === 'transfer'
        && (key === 'handshakeDurationSeconds' || key === 'payloadDurationSeconds')
        ? clientTransferDurationValue(value, key) : metricValue(key, child, unit);
      fields.push({ key: next, label, value: display });
    }
  }
  collect(row.value);

  const currentPage = Math.min(page, Math.max(0, Math.ceil(fields.length / 12) - 1));
  return (
    <Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        Last observed at {resultDate(row.observation.receivedAt)}, sample #{row.observation.index},
        {' '}publication {row.observation.publicationSequence}, topology revision {row.observation.topologyRevision}.
        {!definition && ' The topology definition for this observation is missing.'}
      </Typography>
      <Table size="small" aria-label={`Metrics for ${row.id}`}>
        <TableBody>
          {fields.slice(currentPage * 12, currentPage * 12 + 12).map(field => (
            <TableRow key={field.key}>
              <TableCell component="th" scope="row" sx={{ width: '55%' }}>{field.label}</TableCell>
              <TableCell sx={{ overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>
                {field.value}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {fields.length === 0 && <Typography variant="body2">No measured metrics were received.</Typography>}
      {fields.length > 12 && (
        <TablePagination
          component="div" count={fields.length} page={currentPage} rowsPerPage={12}
          rowsPerPageOptions={[]} onPageChange={(_, next) => setPage(next)}
          labelDisplayedRows={({ from, to, count }) => `${from}–${to} of ${count} metrics`}
        />
      )}
    </Box>
  );
}

function ResourceGroup({ rows, kind, definitions }: {
  rows: readonly Resource[];
  kind: ResourceKind;
  definitions: ReadonlyMap<string, Definition>;
}) {
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const currentPage = Math.min(page, Math.max(0, Math.ceil(rows.length / 10) - 1));
  const resource = rows.find(row => row.id === selected);

  return (
    <Box>
      {rows.length === 0 ? <Typography sx={{ py: 2 }}>No observations for these resources.</Typography> : (
        <>
          <TableContainer>
            <Table size="small" aria-label={`${kind} last observations`}>
              <TableHead>
                <TableRow>
                  <TableCell>Resource / ID</TableCell>
                  <TableCell>Last publication</TableCell>
                  <TableCell>Last virtual time</TableCell>
                  <TableCell>Present in summary sample</TableCell>
                  <TableCell align="right">Action</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.slice(currentPage * 10, currentPage * 10 + 10).map(row => (
                  <TableRow key={row.id} selected={selected === row.id}>
                    <TableCell sx={{ overflowWrap: 'anywhere', maxWidth: 320 }}>
                      {resourceLabel(kind, row, definitions.get(row.observation.topologyRevision))}
                      <Typography variant="caption" display="block" color="text.secondary">{row.id}</Typography>
                    </TableCell>
                    <TableCell>{row.observation.publicationSequence}</TableCell>
                    <TableCell>{approximateValue(row.observation.virtualTimeSeconds, 's')}</TableCell>
                    <TableCell>
                      <Chip size="small" label={row.presentInSummarySample ? 'Yes' : 'No — last seen earlier'} />
                    </TableCell>
                    <TableCell align="right">
                      <Button size="small" aria-label={`Inspect ${row.id}`} onClick={() => setSelected(row.id)}>
                        Inspect
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
          <TablePagination
            component="div" count={rows.length} page={currentPage} rowsPerPage={10}
            rowsPerPageOptions={[]} onPageChange={(_, next) => setPage(next)}
          />
        </>
      )}
      {resource && (
        <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, p: 2, mt: 1 }}>
          <Stack direction="row" justifyContent="space-between" alignItems="center" spacing={1} sx={{ mb: 1 }}>
            <Typography variant="subtitle1" sx={{ overflowWrap: 'anywhere' }}>{resource.id}</Typography>
            <Button size="small" onClick={() => setSelected(null)}>Close metrics</Button>
          </Stack>
          <ResourceMetrics
            key={resource.id} row={resource} kind={kind}
            definition={definitions.get(resource.observation.topologyRevision)}
          />
        </Box>
      )}
    </Box>
  );
}

export default function ResultResources({ result }: { result: RunResult }) {
  const [kind, setKind] = useState<ResourceKind>('nodes');
  const definitions = useMemo(() => new Map(
    result.collection.definitions.map(definition => [definition.revision, definition]),
  ), [result]);
  const resources = result.summary.resources;
  const rows = kind === 'ingress' ? (resources.ingress ? [resources.ingress] : []) : resources[kind];

  return (
    <Accordion defaultExpanded disableGutters variant="outlined">
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Typography variant="h6" component="span">Resource observations</Typography>
      </AccordionSummary>
      <AccordionDetails>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          Each resource keeps its last received value. Absence from the summary sample does not mean zero.
          CPU utilization is the modeled host lifetime mean. Backend p95 measures connection lifetime;
          global transfer p95 / p99 are unavailable.
        </Typography>
        <Tabs
          value={kind} onChange={(_, next: ResourceKind) => setKind(next)}
          variant="scrollable" scrollButtons="auto" aria-label="Resource types"
        >
          {groups.map(group => (
            <Tab
              key={group.kind} value={group.kind}
              label={`${group.label} (${group.kind === 'ingress'
                ? Number(resources.ingress !== null) : resources[group.kind].length})`}
            />
          ))}
        </Tabs>
        <ResourceGroup key={kind} rows={rows} kind={kind} definitions={definitions} />
      </AccordionDetails>
    </Accordion>
  );
}
