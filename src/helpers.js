// Shared helper utilities

import fs from 'node:fs';

export function getStringField(record, field) {
  if (!record || typeof record !== 'object') return '';
  const val = record[field];
  if (val == null) return '';
  if (typeof val === 'string') return val;
  if (typeof val === 'object') {
    if (val.display_value != null) return String(val.display_value);
    if (val.value != null) return String(val.value);
  }
  return String(val);
}

export function formatRecordForDisplay(record, columns) {
  const result = {};

  function extractValue(val) {
    if (val == null) return '';
    if (typeof val === 'string') return val;
    if (typeof val === 'object') {
      if (val.display_value != null && val.display_value !== '') return String(val.display_value);
      if (val.value != null) return String(val.value);
    }
    return String(val);
  }

  if (record.sys_id != null) {
    result.sys_id = extractValue(record.sys_id);
  }

  for (const col of columns) {
    if (record[col] != null) {
      result[col] = extractValue(record[col]);
    } else {
      result[col] = '';
    }
  }
  return result;
}

export function truncateString(str, maxLen) {
  if (!str || str.length <= maxLen) return str;
  return str.slice(0, maxLen - 3) + '...';
}

export function isHexString(str) {
  return /^[0-9a-fA-F]+$/.test(str);
}

export function extractProfileName(instanceURL) {
  let name = instanceURL.replace(/^https?:\/\//, '');
  name = name.replace(/\.service-now\.com$/, '');
  name = name.replace(/\.servicenowservices\.com$/, '');
  return name;
}

export function buildQuerySuffix(query) {
  return query ? ` --query "${query}"` : '';
}

/**
 * Parse --data or --data-file into a JSON object.
 * If --data-file is given, reads the file. Otherwise parses --data directly.
 * Throws if neither is provided or JSON is invalid.
 */
export function parseDataArg(argv) {
  let raw;
  if (argv['data-file']) {
    raw = fs.readFileSync(argv['data-file'], 'utf-8');
  } else if (argv.data) {
    raw = argv.data;
  } else {
    throw new Error('--data or --data-file is required');
  }
  try {
    return JSON.parse(raw);
  } catch (e) {
    throw new Error(`Invalid JSON: ${e.message}\n\nHint: On Windows PowerShell, use --data-file instead of --data to avoid quote mangling.\nRaw value: ${raw.substring(0, 200)}`);
  }
}
