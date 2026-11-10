import { JSONNumber, parseExactJSON } from '../scenarios/json.ts';

// Archive large integers as decimal text; measured floats remain ordinary numbers.
export type ExactTelemetry<T> = T extends number ? number | string
  : T extends readonly (infer Item)[] ? ExactTelemetry<Item>[]
    : T extends object ? { [Key in keyof T]: ExactTelemetry<T[Key]> }
      : T;

const sources = new WeakMap<object, unknown>();

// Decode once for both consumers: numeric live views and exact Results archives.
// For example, a 9007199254740993 byte counter stays that exact string in its source.
export function readTelemetryJSON(document: string): unknown {
  function decode(value: unknown): { view: unknown; source: unknown } {
    if (value instanceof JSONNumber) {
      const numeric = Number(value.text);
      if (!Number.isFinite(numeric)) throw new Error('Telemetry contains a non-finite number.');
      const integer = /^-?(0|[1-9]\d*)$/.test(value.text);
      const source = integer && !Number.isSafeInteger(numeric) ? value.text : numeric;
      return { view: numeric, source };
    }
    if (value === null || typeof value !== 'object') return { view: value, source: value };

    const view: Record<string, unknown> | unknown[] = Array.isArray(value) ? [] : {};
    const source: Record<string, unknown> | unknown[] = Array.isArray(value) ? [] : {};
    for (const [key, child] of Object.entries(value)) {
      const decoded = decode(child);
      // Define own keys safely, including a literal JSON field named __proto__.
      Object.defineProperty(view, key, {
        value: decoded.view, enumerable: true, writable: true, configurable: true,
      });
      Object.defineProperty(source, key, {
        value: decoded.source, enumerable: true, writable: true, configurable: true,
      });
    }
    sources.set(view, source);
    return { view, source };
  }
  return decode(parseExactJSON(document)).view;
}

// Use this before hydration/coalescing when collecting snapshots or HTTP definitions.
// Locally constructed, safe-number fixtures can also be archived without registration.
export function telemetrySource<T extends object>(document: T): ExactTelemetry<T> {
  const source = sources.get(document);
  if (source !== undefined) return source as ExactTelemetry<T>;

  function requireSafeNumbers(value: unknown): void {
    if (typeof value === 'number' && (!Number.isFinite(value)
      || (Number.isInteger(value) && !Number.isSafeInteger(value)))) {
      throw new Error('Exact telemetry source is unavailable for an unsafe numeric value.');
    }
    if (value !== null && typeof value === 'object') {
      Object.values(value).forEach(requireSafeNumbers);
    }
  }
  requireSafeNumbers(document);
  return document as ExactTelemetry<T>;
}
