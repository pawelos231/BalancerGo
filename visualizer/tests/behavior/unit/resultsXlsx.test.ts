import assert from 'node:assert/strict';
import test from 'node:test';
import ExcelJS from 'exceljs';
import { createResultXlsxExport, inspectXlsxExport, XlsxExportError } from '../../../src/results/xlsx.ts';
import { XLSX_HEADERS, XLSX_TABLES, type XlsxLimits } from '../../../src/results/xlsxRows.ts';
import type { RunResult } from '../../../src/results/types.ts';
import { resultFixture } from '../../resultFixture.ts';

type ExportRecord = Record<typeof XLSX_HEADERS[number], string | number | boolean | undefined>;

function mutableFixture() {
  return JSON.parse(JSON.stringify(resultFixture()));
}

async function exported(result: RunResult, limits?: Partial<XlsxLimits>) {
  const download = await createResultXlsxExport(result, { limits });
  const workbook = new ExcelJS.Workbook();
  await workbook.xlsx.load(Buffer.from(await download.blob.arrayBuffer()));
  return { download, workbook, records: readRecords(workbook) };
}

function readRecords(workbook: ExcelJS.Workbook): ExportRecord[] {
  const records = new Map<number, ExportRecord>();
  for (const sheet of workbook.worksheets) {
    assert.deepEqual(sheet.getRow(1).values.slice(1), XLSX_HEADERS);
    sheet.eachRow((row, rowNumber) => {
      if (rowNumber === 1) return;
      const id = row.getCell(1).value as number;
      let record = records.get(id);
      if (!record) {
        record = {} as ExportRecord;
        records.set(id, record);
      }
      for (let index = 0; index < XLSX_HEADERS.length; index++) {
        const key = XLSX_HEADERS[index];
        const value = row.getCell(index + 1).value;
        if (key === 'recordId' || key === 'segment' || key === 'segments') continue;
        if (typeof value === 'string') record[key] = String(record[key] ?? '') + value;
        else if (value !== null) record[key] = value as number | boolean;
      }
    });
  }
  return [...records.entries()].sort(([a], [b]) => a - b).map(([, record]) => {
    for (const column of String(record.encodedColumns ?? '').split(',')) {
      if (column) record[column as keyof ExportRecord] = JSON.parse(String(record[column as keyof ExportRecord]));
    }
    return record;
  });
}

function restoredSource(records: ExportRecord[]): unknown {
  let result: unknown;
  for (const record of records) {
    if (record.sourceType === 'export-note') continue;
    let value: unknown;
    switch (record.sourceType) {
      case 'object': value = {}; break;
      case 'array': value = new Array(Number(record.value)); break;
      case 'null': value = null; break;
      case 'number': value = Number(record.value); break;
      case 'boolean': value = record.value; break;
      case 'string': value = record.value ?? ''; break;
      default: assert.fail(`Unknown source type ${record.sourceType}`);
    }
    const pointer = String(record.sourcePath ?? '');
    if (!pointer) {
      result = value;
      continue;
    }
    const path = pointer.slice(1).split('/').map(part => part.replace(/~1/g, '/').replace(/~0/g, '~'));
    let parent = result as Record<string, unknown>;
    for (const part of path.slice(0, -1)) parent = parent[part] as Record<string, unknown>;
    Object.defineProperty(parent, path[path.length - 1], {
      value, enumerable: true, writable: true, configurable: true,
    });
  }
  return result;
}

function field(records: ExportRecord[], sourcePath: string) {
  const record = records.find(value => value.sourcePath === sourcePath);
  assert.ok(record, `Missing ${sourcePath}`);
  return record;
}

test('XLSX round-trips all nine tables, original JSON fields and exact counters without mutating the archive', async () => {
  const result = mutableFixture();
  result.extra = JSON.parse('{"__proto__":{"retained":true},"a/b~c":[null,{},[],"",true]}');
  result.collection.samples[0].snapshot.servers[0].future = { metrics: [1, { stats: 2 }] };
  result.collection.samples[0].snapshot.servers[0].clients[0].future = [{ metrics: [null, 3] }];
  const before = JSON.stringify(result);
  const { download, workbook, records } = await exported(result);

  assert.deepEqual(workbook.worksheets.map(sheet => sheet.name), XLSX_TABLES);
  assert.deepEqual(restoredSource(records), result);
  assert.equal(JSON.stringify(result), before);
  assert.equal(field(records, '/summary/payload/receivedBytes').value, '18446744073709551615');
  assert.equal(field(records, '/context/scenario/fixed/execution/seed').value, '-9007199254740993');
  const p95 = '/collection/samples/0/snapshot/servers/0/latencyP95Milli';
  assert.equal(field(records, p95).unit, 'ms');
  assert.equal(field(records, p95).backendId, 'srv');
  assert.ok(download.filename.endsWith('.xlsx'));
  assert.equal(download.blob.type, 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet');
  assert.equal(download.stats.dataRows, workbook.worksheets.reduce((sum, sheet) => sum + sheet.rowCount - 1, 0));
  assert.ok(records.some(record => record.sourceType === 'export-note' && record.metric === 'format'));
});

test('Excel numeric precision and formula-like strings preserve original values and types', async () => {
  const result = mutableFixture();
  result.extra = {
    fifteen: 999_999_999_999_999, sixteen: 1_000_000_000_000_000, safeSixteen: 9_007_199_254_740_991,
    measuredFloat: 0.30000000000000004, text: '0001', negativeZero: -0,
    tinyPositive: 1e-309, tinyNegative: -1e-309, smallest: Number.MIN_VALUE, normalSmall: 1e-307,
    formulas: ['=1+1', '+SUM(A1)', '-2+3', '@SUM(A1)', '\t=1', '\n=1', 'https://example.com'],
  };
  const { records, workbook } = await exported(result);
  assert.equal(field(records, '/extra/fifteen').value, 999_999_999_999_999);
  assert.equal(field(records, '/extra/sixteen').value, '1000000000000000');
  assert.equal(field(records, '/extra/safeSixteen').value, '9007199254740991');
  assert.equal(field(records, '/extra/measuredFloat').value, '0.30000000000000004');
  assert.equal(field(records, '/extra/sixteen').sourceType, 'number');

  // Excel would replace these nonzero numbers with zero if they were numeric cells.
  assert.equal(field(records, '/extra/tinyPositive').value, '1e-309');
  assert.equal(field(records, '/extra/tinyNegative').value, '-1e-309');
  assert.equal(field(records, '/extra/smallest').value, String(Number.MIN_VALUE));
  assert.equal(field(records, '/extra/tinyPositive').sourceType, 'number');
  assert.equal(field(records, '/extra/normalSmall').value, 1e-307);

  assert.deepEqual(restoredSource(records), result);
  for (const sheet of workbook.worksheets) {
    sheet.eachRow(row => row.eachCell(cell => {
      assert.notEqual(cell.type, ExcelJS.ValueType.Formula);
      assert.notEqual(cell.type, ExcelJS.ValueType.Hyperlink);
    }));
  }
});

test('unknown strings, paths and IDs containing XML-incompatible characters round-trip losslessly', async () => {
  const result = mutableFixture();
  const key = 'key\u0000/\u007f~\uffff\ud800';
  result.extra = {
    [key]: '\u0000\u000b\u001f\u007f\ufffe\uffff\ud800\udfff 😀', formulas: '=1+1',
    carriageReturn: 'a\rb\r\nc', escapeText: '_x0000_ _x000D_ _x005F_',
    loneHigh: '\ud800', loneLow: '\udfff',
  };
  result.collection.samples[0].snapshot.servers[0].id = 'backend\u0001';
  const { records } = await exported(result);
  assert.deepEqual(restoredSource(records), result);
  const record = records.find(value => value.value === result.extra[key]);
  assert.ok(record);
  assert.match(String(record.encodedColumns), /sourcePath/);
  assert.match(String(record.encodedColumns), /value/);
  assert.equal(field(records, '/extra/formulas').encodedColumns, '');
});

test('availability qualifies unmeasured duration zeros and nulls while retaining each original field', async () => {
  const result = mutableFixture();
  result.summary.transfers.handshake = { samples: 0, meanSeconds: 0, minSeconds: 0, maxSeconds: 0 };
  const transfer = result.collection.samples[0].snapshot.servers[0].clients[0].transfer;
  transfer.handshakeCompleted = false;
  transfer.handshakeDurationSeconds = 0;
  transfer.status = 'inProgress';
  transfer.payloadDurationSeconds = 0;
  result.extra = { count: 1, bytes: 2, samples: 'text', meanSeconds: 0, transfer: { payloadDurationSeconds: 0 } };
  const { records } = await exported(result);
  assert.equal(field(records, '/summary/transfers/handshake/meanSeconds').value, 0);
  assert.equal(field(records, '/summary/transfers/handshake/meanSeconds').availability, 'unavailable');
  const client = '/collection/samples/0/snapshot/servers/0/clients/0/transfer';
  assert.equal(field(records, `${client}/handshakeDurationSeconds`).value, 0);
  assert.equal(field(records, `${client}/handshakeDurationSeconds`).availability, 'unavailable');
  assert.equal(field(records, `${client}/payloadDurationSeconds`).availability, 'unavailable');
  assert.equal(field(records, '/summary/transferLatencyP99Seconds').sourceType, 'null');
  assert.equal(field(records, '/summary/transferLatencyP99Seconds').value, 'Unavailable');
  assert.equal(field(records, '/extra/meanSeconds').availability, 'recorded');
  assert.equal(field(records, '/extra/transfer/payloadDurationSeconds').availability, 'recorded');
  assert.equal(field(records, '/extra/count').unit, undefined);
  assert.equal(field(records, '/extra/bytes').unit, undefined);
  assert.deepEqual(restoredSource(records), result);
});

test('continuation sheets and text chunks reconstruct long paths, IDs and Unicode strings', async () => {
  const result = mutableFixture();
  result.extra = { ['long/~' + '😀'.repeat(80)]: '\u0001' + '😀'.repeat(100) };
  result.collection.samples[0].snapshot.servers[0].id = '😀'.repeat(70);
  const limits = { rowsPerSheet: 80, cellCharacters: 64, maxSheets: 64 };
  const { workbook, records, download } = await exported(result, limits);
  assert.ok(workbook.worksheets.some(sheet => /_2$/.test(sheet.name)));
  assert.ok(download.stats.dataRows > download.stats.logicalRows);
  for (const sheet of workbook.worksheets) {
    assert.ok(sheet.rowCount <= 80);
    sheet.eachRow((row, number) => {
      if (number === 1) return;
      row.eachCell(cell => {
        if (typeof cell.value === 'string') {
          assert.ok(cell.value.length <= 64);
          assert.doesNotMatch(cell.value, /[\ud800-\udbff]$/u);
          assert.doesNotMatch(cell.value, /^[\udc00-\udfff]/u);
        }
      });
    });
  }
  assert.deepEqual(restoredSource(records), result);
});

test('sample identity and definition joins use publication/index and the exact revision', async () => {
  const result = mutableFixture();
  const first = result.collection.samples[0];
  const second = structuredClone(first);
  second.index = 2;
  second.publicationSequence = '4';
  second.snapshot.simulation.publicationSequence = '4';
  second.snapshot.topology.revision = 'missing';
  result.collection.samples.push(second);
  const { records } = await exported(result);
  const link = '/collection/samples/0/snapshot/topology/links/0/stats/deliveredBytes';
  assert.equal(field(records, link).source, 'client-a');
  assert.equal(field(records, link).target, 'lb');
  assert.equal(field(records, link.replace('/samples/0/', '/samples/1/')).source, undefined);
  const metric = '/collection/samples/0/snapshot/topology/nodes/2/metrics/completedStreams';
  assert.equal(field(records, metric).unit, 'streams');
  assert.equal(field(records, metric.replace('/samples/0/', '/samples/1/')).unit, undefined);
  assert.equal(field(records, link).virtualTimeSeconds, field(records, link.replace('/samples/0/', '/samples/1/')).virtualTimeSeconds);
  assert.equal(field(records, link.replace('/samples/0/', '/samples/1/')).sampleIndex, 2);
  assert.equal(field(records, '/summary/resources/ingress/value/deliveredBytes').resourceId, 'ingress');
  assert.equal(field(records, '/summary/resources/ingress/value/deliveredBytes').publicationSequence, '3');
  assert.deepEqual(restoredSource(records), result);
});

test('bounded preflight rejects oversized exports with a full JSON alternative and creates no download', async () => {
  const result = resultFixture();
  for (const limits of [{ maxRows: 9 }, { maxCells: 1 }, { maxTextBytes: 1 }, { maxSheets: 8 }]) {
    assert.throws(() => inspectXlsxExport(result, limits), (error: unknown) => {
      assert.ok(error instanceof XlsxExportError);
      assert.equal(error.code, 'limit');
      assert.match(error.message, /full JSON/);
      return true;
    });
  }
  let reported = false;
  await assert.rejects(createResultXlsxExport(result, { limits: { maxRows: 9 }, onProgress: () => { reported = true; } }), {
    name: 'XlsxExportError', code: 'limit',
  });
  assert.equal(reported, false);
  assert.throws(() => inspectXlsxExport(result, { rowsPerSheet: 1 }), { code: 'invalid' });
  assert.throws(() => inspectXlsxExport(result, { cellCharacters: 32_768 }), { code: 'invalid' });
});

test('summary context identifies nested clients and joins link directions at the resource observation revision', async () => {
  const result = mutableFixture();
  const { records } = await exported(result);
  const backend = '/summary/resources/servers/0';
  assert.equal(field(records, `${backend}/observation/publicationSequence`).resourceKind, 'servers');
  assert.equal(field(records, `${backend}/observation/publicationSequence`).resourceId, 'srv');
  const nestedClient = field(records, `${backend}/value/clients/0/transfer/payloadDurationSeconds`);
  assert.equal(nestedClient.resourceKind, 'client');
  assert.equal(nestedClient.resourceId, 'client-a');
  assert.equal(nestedClient.backendId, 'srv');
  assert.equal(field(records, '/summary/resources/clients/0/value/retrySyn').backendId, 'srv');
  const link = '/summary/resources/links/0/value/stats/deliveredBytes';
  const queue = '/summary/resources/queues/0/value/stats/queuedPackets';
  assert.equal(field(records, link).source, 'client-a');
  assert.equal(field(records, link).target, 'lb');
  assert.equal(field(records, queue).queueSide, 'clients');
  assert.deepEqual(restoredSource(records), result);

  result.summary.resources.links[0].observation.topologyRevision = 'missing';
  result.summary.resources.queues[0].observation.topologyRevision = 'missing';
  const missing = await exported(result);
  assert.equal(field(missing.records, link).source, undefined);
  assert.equal(field(missing.records, link).target, undefined);
  assert.equal(field(missing.records, queue).queueSide, undefined);
});

test('definition ingress queues retain identity, direction and scoped capacity units', async () => {
  const result = mutableFixture();
  result.extra = { capacity: 4 };
  result.collection.samples[0].snapshot.topology.revision = 'missing';
  const { records } = await exported(result);
  const ingress = '/collection/definitions/0/ingress';
  const queue = `${ingress}/queues/0/capacity`;
  assert.equal(field(records, queue).resourceKind, 'queue');
  assert.equal(field(records, queue).resourceId, 'front');
  assert.equal(field(records, queue).queueSide, 'clients');
  assert.equal(field(records, queue).unit, 'packets');
  assert.equal(field(records, `${ingress}/config/queueCapacity`).resourceKind, 'ingress');
  assert.equal(field(records, `${ingress}/config/queueCapacity`).resourceId, 'ingress');
  const missing = '/collection/samples/0/snapshot/topology/ingress/queues/0/stats/queuedPackets';
  assert.equal(field(records, missing).resourceId, 'front');
  assert.equal(field(records, missing).queueSide, undefined);
  assert.equal(field(records, '/extra/capacity').unit, undefined);
  const fixed = '/context/scenario/fixed';
  assert.equal(field(records, `${fixed}/workload/defaults/packets`).unit, '1024 B payload units');
  assert.equal(field(records, `${fixed}/tcp/congestionControl/initialWindowSegments`).unit, 'MSS units');
  assert.equal(field(records, `${fixed}/tcp/congestionControl/maximumWindowSegments`).unit, 'MSS units');
  assert.equal(field(records, `${fixed}/tcp/delayedAck/segmentsPerACK`).unit, 'segments / ACK');
  assert.deepEqual(restoredSource(records), result);
});
