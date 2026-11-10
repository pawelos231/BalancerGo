import {
  fieldAvailability, sourceObject, tableForPath, XlsxFieldMetadata, XLSX_TABLES,
  type FieldContext, type XlsxTable,
} from './xlsxFields.ts';
import type { RunResult } from './types.ts';

export { XLSX_TABLES } from './xlsxFields.ts';
export type { XlsxTable } from './xlsxFields.ts';

export const XLSX_HEADERS = [
  'recordId', 'sourcePath', 'sourceType', 'sampleIndex', 'publicationSequence', 'virtualTimeSeconds',
  'receivedAtUnixMs', 'topologyRevision', 'resourceKind', 'resourceId', 'backendId', 'source', 'target',
  'queueSide', 'metric', 'value', 'unit', 'availability', 'note', 'encodedColumns', 'segment', 'segments',
] as const;

export interface XlsxLimits {
  readonly maxRows: number;
  readonly maxCells: number;
  readonly maxTextBytes: number;
  readonly maxSheets: number;
  readonly rowsPerSheet: number;
  readonly cellCharacters: number;
}

export const DEFAULT_XLSX_LIMITS: XlsxLimits = Object.freeze({
  maxRows: 100_000,
  maxCells: 750_000,
  maxTextBytes: 16 * 1024 * 1024,
  maxSheets: 64,
  rowsPerSheet: 25_000,
  cellCharacters: 32_767,
});

export interface XlsxTablePlan {
  readonly dataRows: number;
  readonly worksheetCount: number;
}

export interface XlsxExportPlan {
  readonly logicalRows: number;
  readonly dataRows: number;
  readonly totalRows: number;
  readonly populatedCells: number;
  readonly textBytes: number;
  readonly worksheetCount: number;
  readonly tables: Readonly<Record<XlsxTable, XlsxTablePlan>>;
  readonly limits: XlsxLimits;
}

export class XlsxExportError extends Error {
  readonly code: 'limit' | 'invalid' | 'generation';

  constructor(code: XlsxExportError['code'], message: string) {
    super(message);
    this.name = 'XlsxExportError';
    this.code = code;
  }
}

export type XlsxCell = string | number | boolean | undefined;

export interface XlsxRow {
  readonly table: XlsxTable;
  readonly cells: readonly XlsxCell[];
  readonly firstSegment: boolean;
}

export function resolveXlsxLimits(overrides: Partial<XlsxLimits> = {}): XlsxLimits {
  const limits = { ...DEFAULT_XLSX_LIMITS, ...overrides };
  for (const [key, value] of Object.entries(limits)) {
    const minimum = key === 'rowsPerSheet' || key === 'cellCharacters' ? 2 : 1;
    if (!Number.isSafeInteger(value) || value < minimum) {
      throw new XlsxExportError('invalid', `Invalid XLSX ${key} limit. Use a positive whole number.`);
    }
  }
  if (limits.rowsPerSheet > 1_048_576 || limits.cellCharacters > 32_767) {
    throw new XlsxExportError('invalid', 'XLSX limits exceed Excel worksheet or cell limits.');
  }
  return Object.freeze(limits);
}

function exactCellNumber(value: number): string | number {
  // Excel preserves fewer integer digits than JavaScript, including some safe JS integers.
  if (Number.isInteger(value) && Math.abs(value) >= 1e15) return String(value);
  if (Object.is(value, -0)) return '-0';

  // Excel cannot store subnormal numbers; text keeps values such as 1e-309 from becoming zero.
  // https://learn.microsoft.com/en-us/troubleshoot/microsoft-365-apps/excel/floating-point-arithmetic-inaccurate-result
  const minimumNormalMagnitude = 2 ** -1022;
  if (value !== 0 && Math.abs(value) < minimumNormalMagnitude) return String(value);

  return Number(value.toPrecision(15)) === value ? value : String(value);
}

function sourceValue(value: unknown): XlsxCell {
  if (value === null) return 'Unavailable';
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) throw new XlsxExportError('invalid', 'The result contains a non-finite number.');
    return exactCellNumber(value);
  }
  if (typeof value === 'string' || typeof value === 'boolean') return value;
  if (Array.isArray(value)) return value.length;
  if (sourceObject(value)) return undefined;
  throw new XlsxExportError('invalid', 'The result contains a value that is not JSON. Export full JSON instead.');
}

function jsonPointer(path: readonly string[]): string {
  return path.map(part => `/${part.replace(/~/g, '~0').replace(/\//g, '~1')}`).join('');
}

function needsEncoding(value: string): boolean {
  // ExcelJS strips controls, and XML cannot carry lone surrogates or the two forbidden BMP characters.
  if (/_x[0-9a-f]{4}_/i.test(value)) return true;
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code <= 8 || code >= 11 && code <= 31 || code === 127 || code === 0xfffe || code === 0xffff) return true;
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++index);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return true;
    } else if (code >= 0xdc00 && code <= 0xdfff) return true;
  }
  return false;
}

function encodeText(value: string): string {
  return JSON.stringify(value)
    .replace(/_(?=x[0-9a-f]{4}_)/gi, '\\u005f')
    .replace(/[\u007f\ufffe\uffff]/g, character => (
      `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`
    ));
}

function textSegments(value: string, maximum: number): string[] {
  const parts: string[] = [];
  let start = 0;
  do {
    let end = Math.min(start + maximum, value.length);
    const last = value.charCodeAt(end - 1);
    const next = value.charCodeAt(end);
    if (last >= 0xd800 && last <= 0xdbff && next >= 0xdc00 && next <= 0xdfff) end--;
    parts.push(value.slice(start, end));
    start = end;
  } while (start < value.length);
  return parts;
}

function* splitRow(table: XlsxTable, cells: XlsxCell[], maximum: number): Generator<XlsxRow> {
  const encoded: string[] = [];
  const segments = Array.from(cells, (value, index) => {
    if (typeof value !== 'string') return value;
    if (needsEncoding(value)) {
      encoded.push(XLSX_HEADERS[index]);
      value = encodeText(value);
    }
    return textSegments(value, maximum);
  });
  segments[19] = textSegments(encoded.join(','), maximum);
  const count = Math.max(1, ...segments.map(value => Array.isArray(value) ? value.length : 1));
  for (let segment = 0; segment < count; segment++) {
    const output = segments.map(value => Array.isArray(value) ? value[segment] : segment === 0 ? value : undefined);
    output[0] = cells[0];
    output[20] = segment + 1;
    output[21] = count;
    yield { table, cells: output, firstSegment: segment === 0 };
  }
}

const EXPORT_NOTES = [
  ['format', 'Result XLSX v1. sourcePath is an escaped JSON Pointer; sourceType preserves original JSON types.'],
  ['continuations', 'Join rows by recordId across table sheets; concatenate each text column in segment order. '
    + 'Numbers and booleans occur only in segment 1. Decode columns named by encodedColumns with JSON.parse after joining.'],
  ['availability', 'Unavailable never means zero. Raw duration fields remain present when their measurement is unavailable. '
    + 'Structure rows preserve objects, arrays and empty containers; array value is its length.'],
  ['units', 'Wire and payload rates use virtual seconds; engine rates use real seconds. CPU ratios are host lifetime means. '
    + 'Backend p95 is connection lifetime in ms; global transfer p95/p99 are unavailable.'],
  ['limits', 'Workload limits bound rows, cells and encoded text, not actual browser heap usage. '
    + 'Every logical table starts a new sheet before its configured row limit.'],
] as const;

export function* resultXlsxRows(result: RunResult, limits: XlsxLimits): Generator<XlsxRow> {
  const metadata = new XlsxFieldMetadata(result);
  const ancestors = new Set<object>();
  let recordId = 0;

  for (const [metric, note] of EXPORT_NOTES) {
    const cells: XlsxCell[] = [];
    cells[0] = ++recordId;
    cells[2] = 'export-note';
    cells[14] = metric;
    cells[18] = note;
    yield* splitRow('Run', cells, limits.cellCharacters);
  }

  function* walk(value: unknown, path: string[], inherited: FieldContext, parent?: Record<string, unknown>): Generator<XlsxRow> {
    if (path.length > 128 || typeof value === 'object' && value !== null && ancestors.has(value)) {
      throw new XlsxExportError('invalid', 'The result contains a cyclic or excessively deep JSON structure.');
    }
    const context = metadata.context(path, value, inherited);
    const key = path[path.length - 1] ?? '';
    const { availability, note } = fieldAvailability(value, path, parent);
    const type = value === null ? 'null' : Array.isArray(value) ? 'array' : typeof value;
    const cells: XlsxCell[] = [
      ++recordId, jsonPointer(path), type, context.sampleIndex, context.publicationSequence,
      context.virtualTimeSeconds === undefined ? undefined : exactCellNumber(context.virtualTimeSeconds),
      context.receivedAtUnixMs === undefined ? undefined : exactCellNumber(context.receivedAtUnixMs),
      context.topologyRevision, context.resourceKind, context.resourceId, context.backendId,
      context.source, context.target, context.queueSide, key, sourceValue(value),
      metadata.unit(path, parent, context), availability, note,
    ];
    yield* splitRow(tableForPath(path), cells, limits.cellCharacters);

    if (value !== null && typeof value === 'object') {
      ancestors.add(value);
      if (Array.isArray(value)) {
        for (let index = 0; index < value.length; index++) {
          yield* walk(value[index], [...path, String(index)], context);
        }
      } else {
        for (const [childKey, child] of Object.entries(value)) {
          yield* walk(child, [...path, childKey], context, value as Record<string, unknown>);
        }
      }
      ancestors.delete(value);
    }
  }

  yield* walk(result, [], {});
}

export function inspectXlsxExport(result: RunResult, overrides?: Partial<XlsxLimits>): XlsxExportPlan {
  const limits = resolveXlsxLimits(overrides);
  const tables = Object.fromEntries(XLSX_TABLES.map(table => [table, { dataRows: 0, worksheetCount: 1 }])) as
    Record<XlsxTable, { dataRows: number; worksheetCount: number }>;
  let logicalRows = 0;
  let dataRows = 0;
  let populatedCells = 0;
  let textBytes = 0;
  let worksheetCount = XLSX_TABLES.length;
  const encoder = new TextEncoder();

  function countCells(cells: readonly XlsxCell[]) {
    for (const value of cells) {
      if (value === undefined) continue;
      populatedCells++;
      if (typeof value === 'string') textBytes += encoder.encode(value).byteLength;
    }
  }

  function check() {
    const exceeded = dataRows + worksheetCount > limits.maxRows ? `row (${limits.maxRows})`
      : populatedCells > limits.maxCells ? `populated cell (${limits.maxCells})`
        : textBytes > limits.maxTextBytes ? `text byte (${limits.maxTextBytes})`
          : worksheetCount > limits.maxSheets ? `worksheet (${limits.maxSheets})` : null;
    if (exceeded) {
      throw new XlsxExportError('limit', `XLSX export exceeds the ${exceeded} limit. Export full JSON to keep every field.`);
    }
  }

  for (let index = 0; index < XLSX_TABLES.length; index++) countCells(XLSX_HEADERS);
  check();
  for (const row of resultXlsxRows(result, limits)) {
    const table = tables[row.table];
    if (table.dataRows > 0 && table.dataRows % (limits.rowsPerSheet - 1) === 0) {
      table.worksheetCount++;
      worksheetCount++;
      countCells(XLSX_HEADERS);
    }
    table.dataRows++;
    dataRows++;
    if (row.firstSegment) logicalRows++;
    countCells(row.cells);
    check();
  }
  for (const table of Object.values(tables)) Object.freeze(table);
  return Object.freeze({
    logicalRows, dataRows, totalRows: dataRows + worksheetCount, populatedCells, textBytes,
    worksheetCount, tables: Object.freeze(tables), limits,
  });
}
