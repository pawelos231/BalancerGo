import type { Worksheet } from 'exceljs';
import { resultExportFilename, type ResultDownload } from './export.ts';
import type { RunResult } from './types.ts';
import {
  inspectXlsxExport, resultXlsxRows, XlsxExportError, XLSX_HEADERS, XLSX_TABLES,
  type XlsxExportPlan, type XlsxLimits, type XlsxTable,
} from './xlsxRows.ts';

export { inspectXlsxExport, XlsxExportError, DEFAULT_XLSX_LIMITS } from './xlsxRows.ts';
export type { XlsxExportPlan, XlsxLimits } from './xlsxRows.ts';

export interface XlsxExportOptions {
  readonly limits?: Partial<XlsxLimits>;
  readonly onProgress?: (rows: number) => void;
}

export interface ResultXlsxDownload extends ResultDownload {
  readonly stats: XlsxExportPlan;
}

const pause = () => new Promise<void>(resolve => setTimeout(resolve, 0));

export async function createResultXlsxExport(
  result: RunResult,
  options: XlsxExportOptions = {},
): Promise<ResultXlsxDownload> {
  // Let React display progress before preflight; excessive exports fail before loading the workbook library.
  await pause();
  const stats = inspectXlsxExport(result, options.limits);
  options.onProgress?.(0);
  await pause();

  try {
    const module = await import('exceljs');
    const ExcelJS = module.default;
    const workbook = new ExcelJS.Workbook();
    workbook.creator = 'Load balancer simulator';
    const current = new Map<XlsxTable, { sheet: Worksheet; part: number; dataRows: number }>();

    function addSheet(table: XlsxTable, part: number) {
      const name = part === 1 ? table : `${table}_${part}`;
      const sheet = workbook.addWorksheet(name, { views: [{ state: 'frozen', ySplit: 1 }] });
      sheet.addRow([...XLSX_HEADERS]);
      sheet.getRow(1).font = { bold: true };
      sheet.columns.forEach((column, index) => {
        column.width = index === 1 || index === 18 ? 48 : index === 15 ? 32 : 22;
        column.alignment = { vertical: 'top', wrapText: true };
      });
      sheet.autoFilter = { from: { row: 1, column: 1 }, to: { row: 1, column: XLSX_HEADERS.length } };
      const state = { sheet, part, dataRows: 0 };
      current.set(table, state);
      return state;
    }

    for (const table of XLSX_TABLES) addSheet(table, 1);
    let rows = 0;
    for (const row of resultXlsxRows(result, stats.limits)) {
      let state = current.get(row.table)!;
      if (state.dataRows === stats.limits.rowsPerSheet - 1) state = addSheet(row.table, state.part + 1);
      // Primitive strings remain literal XLSX text, including formula-like values and URLs.
      state.sheet.addRow([...row.cells]);
      state.dataRows++;
      rows++;
      if (rows % 500 === 0) {
        options.onProgress?.(rows);
        await pause();
      }
    }
    options.onProgress?.(rows);
    for (const sheet of workbook.worksheets) {
      sheet.autoFilter = { from: { row: 1, column: 1 }, to: { row: sheet.rowCount, column: XLSX_HEADERS.length } };
    }
    await pause();
    const buffer = await workbook.xlsx.writeBuffer();
    const bytes = new Uint8Array(buffer);
    return {
      filename: resultExportFilename(result, 'xlsx'),
      blob: new Blob([bytes], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }),
      stats,
    };
  } catch (error) {
    if (error instanceof XlsxExportError) throw error;
    throw new XlsxExportError('generation', 'XLSX generation failed. Try again or export full JSON to keep every field.');
  }
}
