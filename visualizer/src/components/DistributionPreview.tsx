import { Alert, Box, MenuItem, Stack, TextField, Typography, useTheme } from '@mui/material';
import { useEffect, useId, useMemo, useState } from 'react';
import { buildDistributionPreview } from '../docs/distributionPreview.ts';
import type { DistributionPreview as Preview } from '../docs/distributionPreview.ts';
import {
  continuousValueTicks, createPreviewDisplay, densityAxis, formatChartNumber,
} from '../docs/previewDisplay.ts';
import { compileScenario } from '../scenario-language/compile.ts';
import { sourceExamples } from '../scenarios/sourceExamples.ts';
import { familyByKind, familyBySource } from '../scenarios/randomness/registry.ts';

interface Example { id: string; preview?: Preview; error?: string }
interface DistributionPreviewProps { family: string; onReady?: () => void }

// This component belongs to the lazy documentation chunk. Each family compiles
// its canonical library source once; selecting a preset never recompiles it.
export default function DistributionPreview({ family, onReady }: DistributionPreviewProps) {
  const examples = useMemo((): Example[] => {
    try {
      const kind = familyBySource(family)?.kind;
      if (!kind) throw new Error(`Unknown preview family: ${family}.`);
      const source = sourceExamples.find(example => example.id === `${family}-profile-definitions`);
      if (!source) throw new Error(`The canonical ${family} profile definitions example is unavailable.`);
      const scenario = compileScenario(source.source);
      const profiles = Object.entries(scenario.fixed.randomProfiles ?? {}).filter(([, profile]) => profile.kind === kind);
      if (profiles.length === 0) throw new Error(`The canonical ${family} example has no matching definitions.`);
      return profiles.map(([id, profile]) => {
        try { return { id, preview: buildDistributionPreview(profile) }; }
        catch (error) { return { id, error: errorMessage(error) }; }
      });
    } catch (error) {
      return [{ id: 'unavailable', error: errorMessage(error) }];
    }
  }, [family]);
  const [exampleID, setExampleID] = useState('');
  const selected = examples.find(example => example.id === exampleID) ?? examples[0];
  const preview = selected.preview;
  const display = preview && createPreviewDisplay(preview.unitLabel, preview.axisMinimum, preview.axisMaximum);
  const verticalAxis = preview?.kind === 'pdf' && display
    ? densityAxis(preview.densityMaximum * display.scale)
    : { maximum: 1, ticks: [0, 0.25, 0.5, 0.75, 1] };
  const rawDensityMaximum = display ? verticalAxis.maximum / display.scale : 1;
  const parameterSpecs = preview && familyByKind(preview.family)?.parameters;
  const definition = preview && display && preview.parameters.map(({ name, value }) => {
    const specification = parameterSpecs?.find(parameter => parameter.name === name);
    const role = specification?.role;
    const isQuantity = role === 'quantity' || role === 'positiveQuantity' || role === 'integerQuantity';
    const label = isQuantity ? `${display.quantity(value)} ${display.unitLabel}` : value;
    return `${specification?.source ?? name} = ${label}`;
  }).join('; ');

  // Only numerical tick labels are rounded; the caption keeps exact values.
  const horizontalTicks = preview?.kind === 'pdf' && display
    ? continuousValueTicks(preview.minimum, preview.maximum, display.scale)
    : [];
  const diagramID = useId();
  const theme = useTheme();
  const ink = theme.palette.text.secondary;
  const curve = theme.palette.primary.main;
  const paper = theme.palette.background.paper;

  useEffect(() => { onReady?.(); }, [onReady, family]);

  return (
    <Stack spacing={2} sx={{ my: 2, minWidth: 0 }}>
      <TextField
        select fullWidth size="small" label="Preview example"
        value={selected.id} onChange={event => setExampleID(event.target.value)}
      >
        {examples.map(example => <MenuItem key={example.id} value={example.id}>{example.id}</MenuItem>)}
      </TextField>
      {selected.error && <Alert severity="info">{selected.error}</Alert>}
      {preview && display && (
        <Box component="figure" sx={{ m: 0, p: { xs: 1.5, sm: 2 }, border: 1, borderColor: 'divider', borderRadius: 1 }}>
          <Box
            component="svg" viewBox="0 0 700 275" role="img"
            aria-labelledby={`${diagramID}-title ${diagramID}-description`}
            sx={{ display: 'block', width: '100%', height: 'auto' }}
          >
            <title id={`${diagramID}-title`}>{chartTitle(preview)}: {selected.id}</title>
            <desc id={`${diagramID}-description`}>
              {preview.notes.join(' ')} Axis: {display.quantity(preview.axisMinimum)} to {display.quantity(preview.axisMaximum)} {display.unitLabel}.
            </desc>
            <g fill="none" stroke={ink} strokeWidth="1.5"><path d="M70 30 V210 H660" /></g>
            {preview.kind === 'pdf' && (
              <g stroke={curve}>
                <path d={densityPath(preview, rawDensityMaximum)} fill="none" strokeWidth="3" />
                {preview.singularities.map(singularity => {
                  const x = singularity.edge === 'minimum' ? 70 : 660;
                  return <g key={singularity.edge}>
                    <path d={`M${x} 90 V38 m-7 10 7-10 7 10`} fill="none" strokeWidth="2" strokeDasharray="4 3" />
                    <text x={x + (singularity.edge === 'minimum' ? 12 : -12)} y="39"
                      textAnchor={singularity.edge === 'minimum' ? 'start' : 'end'}
                      stroke="none" fill={curve} fontSize="17">→ +∞</text>
                  </g>;
                })}
                {preview.singularities.every(singularity => singularity.edge !== 'minimum') && (
                  <circle cx="70" cy={densityY(preview.points[0].density, rawDensityMaximum)} r="4" fill={curve} />
                )}
                {preview.singularities.every(singularity => singularity.edge !== 'maximum') && (
                  <circle cx="660" cy={densityY(preview.points[preview.points.length - 1].density, rawDensityMaximum)}
                    r="4" fill={paper} strokeWidth="2" />
                )}
              </g>
            )}
            {preview.kind === 'pmf' && (
              <g fill={curve} stroke={curve}>
                {preview.bars.map(bar => {
                  const x = horizontal(bar.position);
                  const y = probabilityY(bar.probability);
                  const width = Math.min(28, 460 / preview.bars.length);
                  return <g key={bar.value}>
                    <title>{display.quantity(bar.value)} {display.unitLabel}: probability {bar.probabilityLabel}</title>
                    <rect x={x - width / 2} y={y} width={width} height={210 - y} fillOpacity="0.22" strokeWidth="1.5" />
                    <circle cx={x} cy={y} r="3" />
                  </g>;
                })}
              </g>
            )}
            {preview.kind === 'ecdf' && (
              <g stroke={curve} fill="none" strokeWidth="2.5">
                {preview.steps.map((step, index) => {
                  const x = horizontal(step.position);
                  const previous = index === 0 ? 70 : horizontal(preview.steps[index - 1].position);
                  return <g key={step.value}>
                    <title>{display.quantity(step.value)} {display.unitLabel}: {step.count} observations; cumulative mass {step.cumulativeLabel}</title>
                    <path d={`M${previous} ${probabilityY(step.before)} H${x}`} />
                    <path d={`M${x} ${probabilityY(step.before)} V${probabilityY(step.after)}`} strokeDasharray="3 4" strokeWidth="1" />
                    <circle cx={x} cy={probabilityY(step.before)} r="4" fill={paper} />
                    <circle cx={x} cy={probabilityY(step.after)} r="4" fill={curve} />
                  </g>;
                })}
                <path d={`M${horizontal(preview.steps[preview.steps.length - 1].position)} ${probabilityY(1)} H660`} />
              </g>
            )}
            <g fill={ink} fontSize="16" fontFamily="sans-serif">
              {verticalAxis.ticks.map(value => <g key={value}>
                <path d={`M65 ${densityY(value, verticalAxis.maximum)} H70`} stroke={ink} />
                <text x="57" y={densityY(value, verticalAxis.maximum) + 5} textAnchor="end" fontSize="13">
                  {preview.kind === 'pdf' ? formatChartNumber(value) : `${value * 100}%`}
                </text>
              </g>)}
              {preview.kind === 'pdf'
                ? horizontalTicks.map(({ position, label }) => <text key={position}
                  x={horizontal(position)} y="238"
                  textAnchor={position === 0 ? 'start' : position === 1 ? 'end' : 'middle'}>
                  {label}
                </text>)
                : <>
                  <text x="70" y="238" textAnchor="start">axis min</text>
                  <text x="660" y="238" textAnchor="end">axis max</text>
                </>}
              <text x="365" y="268" textAnchor="middle">Value ({display.unitLabel})</text>
              <text x="20" y="120" textAnchor="middle" transform="rotate(-90 20 120)">
                {preview.kind === 'pdf' ? `Density (per ${display.unitLabel})` : preview.kind === 'pmf' ? 'Probability mass' : 'Cumulative probability'}
              </text>
            </g>
          </Box>
          <Stack component="figcaption" spacing={1} sx={{ mt: 2, overflowWrap: 'anywhere' }} aria-live="polite">
            <Typography variant="body2">
              <strong>{preview.kind === 'pdf' ? 'Values:' : 'Horizontal axis:'}</strong>{' '}
              {display.quantity(preview.axisMinimum)} to {display.quantity(preview.axisMaximum)} {display.unitLabel}.
              {preview.kind === 'pdf' && ' The filled minimum is included; the open maximum is excluded.'}
            </Typography>
            {preview.parameters.length > 0 && (
              <Typography variant="body2">
                <strong>Definition:</strong> {definition}.
              </Typography>
            )}
            {preview.kind === 'pdf' && <Typography variant="body2">
              <strong>Read the curve:</strong> higher parts mean values nearby are more common.
              Height is density per {display.unitLabel}, not the probability of one exact value.
              {preview.singularities.length > 0 && ' The endpoint arrow continues above this finite drawing.'}
            </Typography>}
            {preview.kind === 'pdf' && preview.singularities.map(singularity => <Typography key={singularity.edge} variant="body2">
              <strong>Singular {singularity.edge}:</strong> density tends to +∞ at {display.quantity(singularity.value)} {display.unitLabel};
              the finite curve {singularity.edge === 'minimum' ? 'starts' : 'ends'} at {display.quantity(singularity.finiteCurveBoundary)} {display.unitLabel}.
            </Typography>)}
            {preview.notes.map(note => <Typography key={note} variant="body2" color="text.secondary">{note}</Typography>)}
          </Stack>
          {preview.kind !== 'pdf' && (
            <Box sx={{ mt: 2, maxHeight: 240, overflow: 'auto' }}>
              <Box component="table" aria-label="Exact values and probabilities" sx={{
                width: '100%',
                borderCollapse: 'collapse',
                fontSize: '0.875rem',
                '& th, & td': {
                  textAlign: 'left', p: 1, borderBottom: 1, borderColor: 'divider',
                  overflowWrap: 'normal',
                },
                '& td': { whiteSpace: 'nowrap' },
              }}>
                <thead><tr>
                  <th scope="col">Value ({display.unitLabel})</th>
                  <th scope="col">{preview.kind === 'pmf' ? 'Probability mass' : 'Count'}</th>
                  {preview.kind === 'ecdf' && <th scope="col">Cumulative probability</th>}
                </tr></thead>
                <tbody>{preview.kind === 'pmf'
                  ? preview.bars.map(bar => <tr key={bar.value}><td>{display.quantity(bar.value)}</td><td>{bar.probabilityLabel}</td></tr>)
                  : preview.steps.map(step => <tr key={step.value}>
                    <td>{display.quantity(step.value)}</td><td>{step.count}</td><td>{step.cumulativeLabel}</td>
                  </tr>)}</tbody>
              </Box>
            </Box>
          )}
        </Box>
      )}
      <Typography variant="body2" color="text.secondary">
        From the library’s {family} profile definitions example. This describes the mathematical definition;
        it predicts no packets or simulation results.
      </Typography>
    </Stack>
  );
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'The analytical preview is unavailable.'; }
function chartTitle(preview: Preview): string {
  return preview.kind === 'pdf' ? 'Analytical density' : preview.kind === 'pmf' ? 'Probability mass' : 'Empirical cumulative distribution';
}
function horizontal(position: number): number { return 70 + 590 * position; }
function probabilityY(probability: number): number { return 210 - 175 * probability; }
function densityY(density: number, maximum: number): number { return 210 - 175 * density / maximum; }
function densityPath(preview: Extract<Preview, { kind: 'pdf' }>, maximum: number): string {
  return preview.points.map((point, index) =>
    `${index === 0 ? 'M' : 'L'}${horizontal(point.position)} ${densityY(point.density, maximum)}`).join(' ');
}
