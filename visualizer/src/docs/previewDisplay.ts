interface Fraction { numerator: bigint; denominator: bigint }

export interface PreviewDisplay {
  readonly unitLabel: string;
  /** Base units in one displayed unit; densities multiply by this scale. */
  readonly scale: number;
  quantity: (exact: string) => string;
  exactDensity: (fractionLabel: string) => string;
}

const units: Readonly<Record<string, readonly string[]>> = {
  ns: ['ns', 'µs', 'ms', 's'],
  B: ['B', 'KB', 'MB', 'GB', 'TB', 'PB'],
  'bit/s': ['bit/s', 'kbit/s', 'Mbit/s', 'Gbit/s', 'Tbit/s', 'Pbit/s'],
};

/** Presentation only: exact model values and normalized chart positions stay unchanged. */
export function createPreviewDisplay(unitLabel: string, minimum: string, maximum: string): PreviewDisplay {
  const lower = fraction(minimum);
  const upper = fraction(maximum);
  const span = {
    numerator: upper.numerator * lower.denominator - lower.numerator * upper.denominator,
    denominator: upper.denominator * lower.denominator,
  };
  // A span preserves readable differences even when values have a huge offset.
  // A single constant has no span, so its magnitude determines the display unit.
  const reference = span.numerator === 0n ? lower : span;
  const magnitude = reference.numerator < 0n ? -reference.numerator : reference.numerator;
  const labels = units[unitLabel] ?? [unitLabel];
  let index = 0;
  let scale = 1n;
  while (index < labels.length - 1 && magnitude >= reference.denominator * scale * 1000n) {
    index += 1;
    scale *= 1000n;
  }

  return {
    unitLabel: labels[index], scale: Number(scale),
    quantity: exact => {
      const value = fraction(exact);
      return decimalText({ numerator: value.numerator, denominator: value.denominator * scale });
    },
    exactDensity: label => {
      const [top, bottom] = label.split('/').map(part => part.trim());
      const value = bottom === undefined ? fraction(top)
        : { numerator: BigInt(top), denominator: BigInt(bottom) };
      return exactLabel({ numerator: value.numerator * scale, denominator: value.denominator });
    },
  };
}

/** A numerical density axis with a little headroom and at most five equal intervals. */
export function densityAxis(maximum: number): { maximum: number; ticks: number[] } {
  if (!(maximum > 0) || !Number.isFinite(maximum)) {
    throw new Error('A density axis requires a positive finite maximum.');
  }
  const power = 10 ** Math.floor(Math.log10(maximum));
  // A subnormal power can underflow although the supplied density is finite.
  if (power === 0) return { maximum, ticks: [0, maximum] };
  const normalized = maximum / power;
  const factor = [1, 2, 2.5, 5, 10].find(candidate => candidate >= normalized) ?? 10;
  const niceMaximum = factor * power;
  if (!Number.isFinite(niceMaximum)) return { maximum, ticks: [0, maximum / 2, maximum] };
  const intervals = factor === 2 ? 4 : 5;
  return {
    maximum: niceMaximum,
    ticks: Array.from({ length: intervals + 1 }, (_, index) => niceMaximum * (index / intervals)),
  };
}

/** Use only for numerical chart ticks, never exact model values or table labels. */
export function formatChartNumber(value: number): string {
  return Number(value.toPrecision(4)).toString();
}

// A narrow range at a huge offset can round every tick to the same number.
// In that case, leave the exact bounds in the caption and mark only min/max.
export function continuousValueTicks(minimum: string, maximum: string, scale: number) {
  const lower = Number(minimum);
  const width = Number(maximum) - lower;
  const ticks = [0, 0.25, 0.5, 0.75, 1].map(position => ({
    position,
    label: formatChartNumber((lower + position * width) / scale),
  }));
  const repeatedLabels = new Set(ticks.map(tick => tick.label)).size < ticks.length;
  return repeatedLabels
    ? [{ position: 0, label: 'min' }, { position: 1, label: 'max' }]
    : ticks;
}

function fraction(value: string): Fraction {
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value);
  if (!match) throw new Error('A preview label must be an exact decimal.');
  const [, sign, whole, decimals = ''] = match;
  return {
    numerator: BigInt(whole + decimals) * (sign ? -1n : 1n),
    denominator: 10n ** BigInt(decimals.length),
  };
}

function reduce(value: Fraction): Fraction {
  if (value.denominator === 0n) throw new Error('An exact density cannot have a zero denominator.');
  const sign = value.denominator < 0n ? -1n : 1n;
  const numerator = value.numerator * sign;
  const denominator = value.denominator * sign;
  let first = numerator < 0n ? -numerator : numerator;
  let second = denominator;
  while (second !== 0n) [first, second] = [second, first % second];
  return { numerator: numerator / first, denominator: denominator / first };
}

function exactLabel(value: Fraction): string {
  const reduced = reduce(value);
  let remainder = reduced.denominator;
  while (remainder % 2n === 0n) remainder /= 2n;
  while (remainder % 5n === 0n) remainder /= 5n;
  return remainder === 1n ? decimalText(reduced) : `${reduced.numerator} / ${reduced.denominator}`;
}

// These denominators contain only factors of two and five, so every digit is exact.
function decimalText(value: Fraction): string {
  const negative = value.numerator < 0n;
  const absolute = negative ? -value.numerator : value.numerator;
  const whole = absolute / value.denominator;
  let remainder = absolute % value.denominator;
  let decimals = '';
  while (remainder !== 0n) {
    remainder *= 10n;
    decimals += remainder / value.denominator;
    remainder %= value.denominator;
  }
  return `${negative ? '-' : ''}${whole}${decimals ? `.${decimals}` : ''}`;
}
