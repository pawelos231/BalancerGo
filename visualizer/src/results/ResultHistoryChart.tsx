import { useId, useMemo, useState } from 'react';
import type { KeyboardEvent, MouseEvent } from 'react';
import { Box, Card, CardContent, Stack, Typography, useMediaQuery, useTheme } from '@mui/material';
import type { ChartDefinition, ChartSample } from './chartData';
import { reduceChartTraces } from './chartData';
import { approximateValue } from './presentation';

const colors = ['#1976d2', '#d66a00', '#258438', '#9b4aaf', '#c13b53'];

function axisValue(value: number): string {
  if (value !== 0 && (Math.abs(value) < 0.001 || Math.abs(value) >= 10_000_000)) return value.toExponential(2);
  return new Intl.NumberFormat('en-US', { maximumSignificantDigits: 4 }).format(value);
}

function chartBounds(samples: readonly ChartSample[], definition: ChartDefinition) {
  let minimum = Infinity;
  let maximum = -Infinity;
  let peak = 0;
  for (const sample of samples) {
    minimum = Math.min(minimum, sample.virtualTimeSeconds);
    maximum = Math.max(maximum, sample.virtualTimeSeconds);
    for (const series of definition.series) peak = Math.max(peak, sample.values[series.key] ?? 0);
  }
  if (!samples.length) return { minimum: 0, maximum: 1, peak: 1 };
  return { minimum, maximum: maximum === minimum ? maximum + 1 : maximum, peak: peak || 1 };
}

function SampleInspection({ sample, definition }: { sample: ChartSample; definition: ChartDefinition }) {
  return (
    <Box sx={{ overflowWrap: 'anywhere' }}>
      <Typography variant="body2">
        Sample #{sample.index}, publication {sample.publicationSequence},
        {' '}{approximateValue(sample.virtualTimeSeconds, 'virtual s')}, revision {sample.topologyRevision}.
      </Typography>
      <Stack direction="row" spacing={2} flexWrap="wrap" useFlexGap>
        {definition.series.map(series => (
          <Typography key={series.key} variant="caption">
            {series.label}: {approximateValue(sample.values[series.key], definition.unit)}
          </Typography>
        ))}
      </Stack>
      {definition.series.some(series => series.key === 'wireRX') && (
        <Typography variant="caption" display="block" color="text.secondary">
          Wire window: {approximateValue(sample.wireWindowSeconds, 'virtual s')};
          {' '}ACK interval: {approximateValue(sample.ackIntervalSeconds, 'virtual s')}.
          {sample.wireWindowSeconds === 0 && ' Zero wire window has no rate measurement.'}
        </Typography>
      )}
      {sample.notes.length > 0 && (
        <Typography variant="caption" display="block" color="text.secondary">{sample.notes.join(' ')}</Typography>
      )}
    </Box>
  );
}

export default function ResultHistoryChart({ samples, definition }: {
  samples: readonly ChartSample[];
  definition: ChartDefinition;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const theme = useTheme();
  const compact = useMediaQuery(theme.breakpoints.down('sm'));
  const [activeSequence, setActiveSequence] = useState<string | null>(null);
  const display = useMemo(() => reduceChartTraces(samples, definition.series), [samples, definition]);
  const displayedSequences = useMemo(() => new Set(display.traces.flatMap(trace => (
    trace.segments.flatMap(segment => segment.map(vertex => vertex.sample.publicationSequence))
  ))), [display]);
  const inspectable = useMemo(() => samples.filter(sample => (
    displayedSequences.has(sample.publicationSequence)
  )), [samples, displayedSequences]);
  const activePosition = inspectable.findIndex(sample => sample.publicationSequence === activeSequence);
  const active = inspectable[activePosition];
  const bounds = useMemo(() => chartBounds(samples, definition), [samples, definition]);
  const width = compact ? 360 : 720;
  const height = 230;
  const left = compact ? 56 : 76;
  const right = width - 18;
  const top = 14;
  const bottom = height - 42;
  const x = (time: number) => left + (time - bounds.minimum) / (bounds.maximum - bounds.minimum) * (right - left);
  const y = (value: number) => bottom - value / bounds.peak * (bottom - top);

  function hover(event: MouseEvent<SVGSVGElement>) {
    const screenTransform = event.currentTarget.getScreenCTM();
    if (!screenTransform) return;

    // Convert the pointer to SVG coordinates, including any space around the plot.
    const pointer = new DOMPoint(event.clientX, event.clientY);
    const position = pointer.matrixTransform(screenTransform.inverse()).x;
    let closest: ChartSample | undefined;
    for (const sample of inspectable) {
      if (!closest || Math.abs(x(sample.virtualTimeSeconds) - position) < Math.abs(x(closest.virtualTimeSeconds) - position)) {
        closest = sample;
      }
    }
    if (closest) setActiveSequence(closest.publicationSequence);
  }

  function keyboard(event: KeyboardEvent<SVGSVGElement>) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key) || !inspectable.length) return;
    event.preventDefault();

    let nextPosition = activePosition < 0 ? 0 : activePosition;
    if (event.key === 'Home') nextPosition = 0;
    else if (event.key === 'End') nextPosition = inspectable.length - 1;
    else if (activePosition >= 0) nextPosition += event.key === 'ArrowRight' ? 1 : -1;

    const boundedPosition = Math.max(0, Math.min(nextPosition, inspectable.length - 1));
    setActiveSequence(inspectable[boundedPosition].publicationSequence);
  }

  return (
    <Card variant="outlined" sx={{ minWidth: 0 }}>
      <CardContent>
        <Typography variant="h6" component="h4" id={titleId}>{definition.title}</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>{definition.description}</Typography>
        <Stack direction="row" gap={1.5} flexWrap="wrap" sx={{ my: 1.5 }}>
          {definition.series.map((series, index) => (
            <Typography key={series.key} variant="caption" sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
              <Box component="span" sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: colors[index] }} />
              {series.label}
              {!display.measuredSeriesKeys.includes(series.key) && ': Unavailable'}
            </Typography>
          ))}
        </Stack>
        <Typography variant="caption" color="text.secondary">Approximate plot values · {definition.unit}</Typography>
        {display.observedPoints === 0 ? (
          <Typography sx={{ py: 3 }}>Unavailable — no measurements for this chart in the retained samples.</Typography>
        ) : (
          <Box
            component="svg" viewBox={`0 0 ${width} ${height}`} role="img" tabIndex={0}
            aria-labelledby={titleId} aria-describedby={descriptionId}
            onMouseMove={hover} onKeyDown={keyboard}
            onFocus={() => { if (!active && inspectable.length) setActiveSequence(inspectable[0].publicationSequence); }}
            sx={{ display: 'block', width: '100%', height: 230, touchAction: 'pan-y', outlineOffset: 2 }}
          >
            <title>{definition.title}; virtual time in seconds; {definition.unit}</title>
            {[0, 0.5, 1].map(fraction => (
              <g key={fraction}>
                <line x1={left} x2={right} y1={y(bounds.peak * fraction)} y2={y(bounds.peak * fraction)}
                  stroke="currentColor" opacity={0.15} />
                <text x={left - 7} y={y(bounds.peak * fraction) + 4} textAnchor="end" fontSize={11} fill="currentColor">
                  {axisValue(bounds.peak * fraction)}
                </text>
                <text x={left + fraction * (right - left)} y={bottom + 19}
                  textAnchor={fraction === 0 ? 'start' : fraction === 1 ? 'end' : 'middle'} fontSize={11} fill="currentColor">
                  {axisValue(bounds.minimum + fraction * (bounds.maximum - bounds.minimum))}
                </text>
              </g>
            ))}
            <text x={(left + right) / 2} y={height - 3} textAnchor="middle" fontSize={11} fill="currentColor">
              Virtual time (s)
            </text>
            {display.traces.map((trace, index) => (
              <g key={trace.key} fill={colors[index]} stroke={colors[index]}>
                {trace.segments.map((segment, segmentIndex) => segment.length === 1 ? (
                  <circle key={segmentIndex} cx={x(segment[0].sample.virtualTimeSeconds)} cy={y(segment[0].value)} r={3} />
                ) : (
                  <path key={segmentIndex} fill="none" strokeWidth={2} strokeLinejoin="round"
                    d={segment.map((vertex, vertexIndex) => (
                      `${vertexIndex === 0 ? 'M' : 'L'}${x(vertex.sample.virtualTimeSeconds)},${y(vertex.value)}`
                    )).join(' ')} />
                ))}
              </g>
            ))}
            {active && (
              <line x1={x(active.virtualTimeSeconds)} x2={x(active.virtualTimeSeconds)} y1={top} y2={bottom}
                stroke="currentColor" strokeDasharray="4 4" opacity={0.7} />
            )}
          </Box>
        )}
        <Typography id={descriptionId} variant="caption" color="text.secondary" display="block" sx={{ my: 1 }}>
          Hover to inspect a displayed sample; focus the chart and use Left / Right, Home or End.
          {' '}Lines stop at missing publications, connection interruptions and unavailable resource measurements.
          {display.displayedPoints < display.observedPoints && (
            <> Display reduced to {display.displayedPoints} of {display.observedPoints} measured series points;
              {' '}all {samples.length} stored samples remain unchanged.</>
          )}
        </Typography>
        <Box aria-live="polite" aria-atomic="true" sx={{ minHeight: 68 }}>
          {active ? <SampleInspection sample={active} definition={definition} /> : (
            <Typography variant="body2" color="text.secondary">No sample selected.</Typography>
          )}
        </Box>
      </CardContent>
    </Card>
  );
}
