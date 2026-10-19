/**
 * Client-side CSV export of rows already on screen.
 *
 * Deliberately scoped to the loaded page: the API has no export endpoint and
 * no unbounded page size, so "export everything" would be a promise the
 * backend cannot keep. Every call site labels the control accordingly.
 */
function escapeCell(value) {
  if (value === null || value === undefined) return '';
  const text = String(value);
  // A leading =, +, - or @ is interpreted as a formula by spreadsheet apps.
  const guarded = /^[=+\-@]/.test(text) ? `'${text}` : text;
  return /[",\n\r]/.test(guarded) ? `"${guarded.replace(/"/g, '""')}"` : guarded;
}

export function exportRowsToCsv({ filename, columns, rows }) {
  const header = columns.map((column) => escapeCell(column.header)).join(',');
  const body = rows.map((row) =>
    columns.map((column) => escapeCell(column.value(row))).join(','),
  );

  // BOM so Excel reads UTF-8 identifiers correctly.
  const blob = new Blob([`﻿${[header, ...body].join('\r\n')}\r\n`], {
    type: 'text/csv;charset=utf-8',
  });

  downloadBlob(blob, filename);
}

/** Save text the page generated - a template, a script - as a file. */
export function downloadText(text, filename, type = 'text/plain') {
  downloadBlob(new Blob([text], { type }), filename);
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export function timestampedName(prefix) {
  const now = new Date();
  const stamp = now.toISOString().slice(0, 16).replace(/[:T]/g, '-');
  return `${prefix}-${stamp}.csv`;
}
