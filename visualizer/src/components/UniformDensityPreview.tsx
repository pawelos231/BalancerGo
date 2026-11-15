import { Box, MenuItem, Stack, TextField, Typography, useTheme } from '@mui/material';
import { useEffect, useId, useState } from 'react';
import { buildDensityPreview } from '../docs/densityPreview.ts';
import { compileScenario } from '../scenario-language/compile.ts';
import type { RandomProfile } from '../scenarios/randomness.ts';
import canonicalSource from '../scenarios/examples/sources/uniform-profile-definitions.lb?raw';

// Read the same source as the library once, when this documentation chunk loads.
const canonicalScenario = compileScenario(canonicalSource);

function namedProfile(id: string): RandomProfile {
  const profile = canonicalScenario.fixed.randomProfiles?.[id];
  if (!profile) throw new Error(`The canonical uniform example is missing ${id}.`);
  return profile;
}

const backendCPU = canonicalScenario.states['0'].resources.backends[0].host?.packetProcessingTimeNanoseconds ?? 0;
const examples = [
  { id: 'cpu-time', label: 'CPU-time definition', profile: namedProfile('cpu-time') },
  { id: 'travel-time', label: 'Travel-time definition', profile: namedProfile('travel-time') },
  { id: 'precise-bytes', label: 'Precise byte definition', profile: namedProfile('precise-bytes') },
  {
    id: 'constant-cpu', label: 'Current scalar backend CPU',
    profile: { kind: 'constant', unit: 'nanoseconds', value: String(backendCPU) } as const,
  },
].map(example => ({ ...example, preview: buildDensityPreview(example.profile) }));

interface UniformDensityPreviewProps {
  onReady?: () => void;
}

export default function UniformDensityPreview({ onReady }: UniformDensityPreviewProps) {
  const [exampleID, setExampleID] = useState(examples[0].id);
  const selected = examples.find(example => example.id === exampleID) ?? examples[0];
  const preview = selected.preview;
  const diagramID = useId();
  const theme = useTheme();
  const ink = theme.palette.text.secondary;
  const curve = theme.palette.primary.main;
  const paper = theme.palette.background.paper;
  const uniform = preview.kind === 'uniform';

  useEffect(() => { onReady?.(); }, [onReady]);

  return (
    <Stack spacing={2} sx={{ my: 2, minWidth: 0 }}>
      <TextField
        select fullWidth size="small" label="Preview example"
        value={exampleID} onChange={event => setExampleID(event.target.value)}
      >
        {examples.map(example => <MenuItem key={example.id} value={example.id}>{example.label}</MenuItem>)}
      </TextField>

      <Box
        component="figure"
        sx={{ m: 0, p: { xs: 1.5, sm: 2 }, border: 1, borderColor: 'divider', borderRadius: 1, minWidth: 0 }}
      >
        <Box
          component="svg" viewBox="0 0 640 250" role="img"
          aria-labelledby={`${diagramID}-title ${diagramID}-description`}
          sx={{ display: 'block', width: '100%', height: 'auto', overflow: 'visible' }}
        >
          <title id={`${diagramID}-title`}>
            {uniform ? `Uniform density: ${selected.label}` : `Constant value: ${selected.label}`}
          </title>
          <desc id={`${diagramID}-description`}>
            {preview.kind === 'uniform'
              ? `Flat density ${preview.density} per ${preview.unitLabel} from included minimum ${preview.minimum} to excluded maximum ${preview.maximum}. The complete support is shown.`
              : `A single fixed value of ${preview.value} ${preview.unitLabel}, with probability one. No finite density curve is drawn.`}
          </desc>

          {uniform ? (
            <g>
              <path d="M60 40 V190 H600" fill="none" stroke={ink} strokeWidth="1.5" />
              <rect x="150" y="70" width="360" height="120" fill={curve} fillOpacity="0.12" />
              <path d="M150 70 V190 M510 70 V190" stroke={curve} strokeDasharray="5 5" />
              <path d="M60 190 H150 V70 H510 V190 H600" fill="none" stroke={curve} strokeWidth="3" />
              <circle cx="150" cy="70" r="5" fill={curve} />
              <circle cx="510" cy="70" r="5" fill={paper} stroke={curve} strokeWidth="3" />
              <g fill={ink} fontSize="18" fontFamily="sans-serif">
                <text x="48" y="196" textAnchor="end">0</text>
                <text x="48" y="76" textAnchor="end">h</text>
                <text x="150" y="220" textAnchor="middle">min</text>
                <text x="510" y="220" textAnchor="middle">max</text>
                <text x="330" y="247" textAnchor="middle">Value ({preview.unitLabel})</text>
                <text x="18" y="115" textAnchor="middle" transform="rotate(-90 18 115)">Density</text>
              </g>
            </g>
          ) : (
            <g>
              <path d="M60 130 H600" fill="none" stroke={ink} strokeWidth="1.5" />
              <circle cx="330" cy="130" r="7" fill={curve} />
              <g fill={ink} fontSize="18" fontFamily="sans-serif" textAnchor="middle">
                <text x="330" y="100">One fixed value</text>
                <text x="330" y="165">Value ({preview.unitLabel})</text>
              </g>
            </g>
          )}
        </Box>

        <Stack component="figcaption" spacing={1} sx={{ mt: 1, overflowWrap: 'anywhere' }} aria-live="polite">
          {preview.kind === 'uniform' ? (
            <>
              <Typography variant="body2">
                <strong>Support:</strong> [{preview.minimum}, {preview.maximum}) {preview.unitLabel}.
                The filled endpoint includes min; the open endpoint excludes max.
              </Typography>
              <Typography variant="body2">
                <strong>Density h:</strong> {preview.density} per {preview.unitLabel}.
                The shaded area is 1. The vertical axis runs from 0 to 1.25 × h.
              </Typography>
            </>
          ) : (
            <Typography variant="body2">
              <strong>Fixed value:</strong> {preview.value} {preview.unitLabel}, with probability 1.
              The dot marks a value; its height does not represent a finite density.
            </Typography>
          )}
          <Typography variant="body2">
            <strong>Horizontal axis:</strong> {preview.axisMinimum} to {preview.axisMaximum} {preview.unitLabel}.
            The entire support is visible, with padding and no truncation.
          </Typography>
        </Stack>
      </Box>

      <Typography variant="body2" color="text.secondary">
        From the library’s Uniform profile definitions example. The curve shows the mathematical definition
        before integer flooring or floating-point conversion. It uses no random samples and predicts no simulation results.
      </Typography>
      {exampleID === 'precise-bytes' && (
        <Typography variant="body2" color="text.secondary">
          These byte bounds remain distinct exact decimals. Integer sampling returns the same whole byte value
          throughout this interval; a floating-point sampler cannot represent a value inside it.
        </Typography>
      )}
    </Stack>
  );
}
