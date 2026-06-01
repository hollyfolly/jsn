import { describe, it } from 'node:test';
import assert from 'node:assert';
import { Writable } from 'node:stream';

function makeStream() {
  let data = '';
  const stream = new Writable({
    write(chunk, encoding, callback) {
      data += chunk.toString();
      callback();
    },
  });
  return { stream, getData: () => data };
}

describe('hyperlink', () => {
  it('wraps text in OSC 8 hyperlink', async () => {
    const { hyperlink } = await import('../src/output.js');
    const result = hyperlink('INC001', 'https://instance/incident.do?sys_id=abc');
    assert.strictEqual(result, '\x1b]8;;https://instance/incident.do?sys_id=abc\x07INC001\x1b]8;;\x07');
  });

  it('returns text unchanged when url is empty', async () => {
    const { hyperlink } = await import('../src/output.js');
    assert.strictEqual(hyperlink('text', ''), 'text');
    assert.strictEqual(hyperlink('text', null), 'text');
  });
});

describe('isTTY', () => {
  it('returns false for non-TTY stream', async () => {
    const { isTTY } = await import('../src/output.js');
    assert.strictEqual(isTTY({ isTTY: false }), false);
  });

  it('returns true for TTY stream', async () => {
    const { isTTY } = await import('../src/output.js');
    assert.strictEqual(isTTY({ isTTY: true }), true);
  });
});

describe('OutputWriter - JSON format', () => {
  it('writes JSON envelope with ok field', async () => {
    const { OutputWriter, FormatJSON } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatJSON, writer: stream });
    writer.ok({ result: 'success' });
    const output = JSON.parse(getData());
    assert.strictEqual(output.ok, true);
    assert.deepStrictEqual(output.data, { result: 'success' });
  });

  it('includes summary in JSON envelope', async () => {
    const { OutputWriter, FormatJSON } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatJSON, writer: stream });
    writer.ok({ key: 'val' }, { summary: 'test summary' });
    const output = JSON.parse(getData());
    assert.strictEqual(output.summary, 'test summary');
  });

  it('includes breadcrumbs in JSON envelope', async () => {
    const { OutputWriter, FormatJSON } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatJSON, writer: stream });
    writer.ok({}, { breadcrumbs: [{ action: 'test', cmd: 'jsn test', description: 'Test' }] });
    const output = JSON.parse(getData());
    assert.strictEqual(output.breadcrumbs.length, 1);
    assert.strictEqual(output.breadcrumbs[0].action, 'test');
  });

  it('writes error as JSON envelope', async () => {
    const { OutputWriter, FormatJSON } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatJSON, writer: stream });
    const error = { code: 'usage_error', message: 'Bad input', hint: 'Fix it' };
    writer.err(error);
    const output = JSON.parse(getData());
    assert.strictEqual(output.ok, false);
    assert.strictEqual(output.error, 'Bad input');
    assert.strictEqual(output.code, 'usage_error');
  });
});

describe('OutputWriter - Markdown format', () => {
  it('writes records as markdown table', async () => {
    const { OutputWriter, FormatMarkdown } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatMarkdown, writer: stream });
    writer.ok({
      table: 'incident',
      count: 2,
      columns: ['number', 'state'],
      records: [
        { number: 'INC001', state: 'In Progress' },
        { number: 'INC002', state: 'Closed' },
      ],
    });
    const md = getData();
    assert.ok(md.includes('| number | state |'));
    assert.ok(md.includes('| INC001 | In Progress |'));
    assert.ok(md.includes('| INC002 | Closed |'));
  });

  it('writes error in markdown', async () => {
    const { OutputWriter, FormatMarkdown } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatMarkdown, writer: stream });
    writer.err({ code: 'not_found', message: 'not found', hint: 'check it' });
    const md = getData();
    assert.ok(md.includes('**Error (not_found)**'));
    assert.ok(md.includes('check it'));
  });
});

describe('OutputWriter - Quiet format', () => {
  it('writes data without envelope', async () => {
    const { OutputWriter, FormatQuiet } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatQuiet, writer: stream });
    writer.ok({ raw: 'data' });
    const output = JSON.parse(getData());
    assert.strictEqual(output.raw, 'data');
    assert.strictEqual(output.ok, undefined);
  });
});

describe('OutputWriter - Styled format', () => {
  it('writes records table with header and separator', async () => {
    const { OutputWriter, FormatStyled } = await import('../src/output.js');
    const { stream, getData } = makeStream();
    const writer = new OutputWriter({ format: FormatStyled, writer: stream });
    writer.ok({
      table: 'incident',
      count: 1,
      columns: ['number', 'state'],
      records: [{ number: 'INC001', state: 'In Progress' }],
      context: { instance_url: 'https://instance' },
    });
    const out = getData();
    assert.ok(out.includes('number'));
    assert.ok(out.includes('state'));
    assert.ok(out.includes('INC001'));
  });
});

describe('getDisplayValue', () => {
  it('handles string values', async () => {
    const { getDisplayValue } = await import('../src/output.js');
    assert.strictEqual(getDisplayValue('hello'), 'hello');
  });

  it('handles null/undefined', async () => {
    const { getDisplayValue } = await import('../src/output.js');
    assert.strictEqual(getDisplayValue(null), '');
    assert.strictEqual(getDisplayValue(undefined), '');
  });

  it('extracts display_value from objects', async () => {
    const { getDisplayValue } = await import('../src/output.js');
    assert.strictEqual(getDisplayValue({ display_value: 'Active', value: '1' }), 'Active');
  });

  it('falls back to value', async () => {
    const { getDisplayValue } = await import('../src/output.js');
    assert.strictEqual(getDisplayValue({ value: 'sysid123' }), 'sysid123');
  });

  it('stringifies other types', async () => {
    const { getDisplayValue } = await import('../src/output.js');
    assert.strictEqual(getDisplayValue(42), '42');
    assert.strictEqual(getDisplayValue(true), 'true');
  });
});

describe('Output format resolution', () => {
  it('auto-detect defaults to JSON for non-TTY', async () => {
    const { OutputWriter } = await import('../src/output.js');
    const { stream } = makeStream();
    const writer = new OutputWriter({ format: 'auto', writer: stream });
    assert.strictEqual(writer.effectiveFormat(), 'json');
  });

  it('explicit format is always used', async () => {
    const { OutputWriter, FormatStyled } = await import('../src/output.js');
    const { stream } = makeStream();
    const writer = new OutputWriter({ format: FormatStyled, writer: stream });
    assert.strictEqual(writer.effectiveFormat(), 'styled');
  });
});
