import { formatRecordForDisplay, buildQuerySuffix, parseDataArg } from '../helpers.js';

const tableDefaultColumns = {
  incident: ['number', 'short_description', 'priority', 'state', 'assigned_to'],
  change_request: ['number', 'short_description', 'risk', 'state', 'assigned_to'],
  change_task: ['number', 'short_description', 'state', 'assigned_to'],
  problem: ['number', 'short_description', 'priority', 'state', 'assigned_to'],
  sc_request: ['number', 'short_description', 'request_state', 'requested_for'],
  sc_req_item: ['number', 'short_description', 'stage', 'assigned_to'],
  sc_task: ['number', 'short_description', 'state', 'assigned_to'],
  sys_user: ['user_name', 'name', 'email', 'active'],
  sys_user_group: ['name', 'manager', 'email'],
  cmdb_ci: ['name', 'operational_status', 'ip_address'],
  cmdb_ci_server: ['name', 'operational_status', 'ip_address'],
  kb_knowledge: ['number', 'short_description', 'workflow_state', 'author'],
};

function getDefaultColumns(table) {
  return tableDefaultColumns[table] || ['sys_id'];
}

export function recordsCmd(wrap) {
  return {
    command: 'records <subcommand>',
    describe: 'Query and manage records in any table (e.g. "records list --table incident --query priority=1")',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'list',
          describe: 'List records from a table',
          builder: (y) => y
            .option('table', { type: 'string', demandOption: true, describe: 'Table name' })
            .option('query', { type: 'string', describe: 'Encoded query (e.g. "nameLIKEincident" or "active=true")' })
            .option('columns', { alias: 'c', type: 'string', describe: 'Comma-separated columns (e.g. "number,short_description")' })
            .option('limit', { type: 'number', default: 20, describe: 'Max records' })
            .option('offset', { type: 'number', default: 0, describe: 'Offset' }),
          handler: wrap(async (argv, app) => {
            const table = argv.table;
            const columns = argv.columns ? argv.columns.split(',') : getDefaultColumns(table);
            const params = new URLSearchParams();
            params.set('sysparm_limit', String(argv.limit));
            params.set('sysparm_offset', String(argv.offset));
            params.set('sysparm_display_value', 'all');
            params.set('sysparm_fields', ['sys_id', ...columns].join(','));
            if (argv.query) params.set('sysparm_query', argv.query);
            const records = await app.sdk.list(table, params);
            const displayRecords = records.map(r => formatRecordForDisplay(r, columns));
            const breadcrumbs = [
              { action: 'create', cmd: `jsn records create --table ${table} --data '{...}'`, description: 'Create a new record' },
              { action: 'filter', cmd: `jsn records list --table ${table} --query "priority=1"`, description: 'Filter: priority 1 only' },
              { action: 'columns', cmd: `jsn dev columns --table ${table}`, description: 'View available columns' },
            ];
            if (records.length === argv.limit) {
              breadcrumbs.push({
                action: 'next',
                cmd: `jsn records list --table ${table} --limit ${argv.limit} --offset ${argv.offset + argv.limit}${buildQuerySuffix(argv.query)}`,
                description: `Next page`,
              });
            }
            if (argv.offset > 0) {
              breadcrumbs.push({
                action: 'prev',
                cmd: `jsn records list --table ${table} --limit ${argv.limit} --offset ${Math.max(0, argv.offset - argv.limit)}${buildQuerySuffix(argv.query)}`,
                description: 'Previous page',
              });
            }
            app.ok({
              table,
              count: records.length,
              columns,
              records: displayRecords,
              pagination: { limit: argv.limit, offset: argv.offset },
              context: { instance_url: app.getEffectiveInstance() },
            }, { summary: `${records.length} record(s) from ${table}`, breadcrumbs });
          }),
        })
        .command({
          command: 'get',
          describe: 'Get a single record by sys_id',
          builder: (y) => y
            .option('table', { type: 'string', demandOption: true, describe: 'Table name' })
            .option('sys-id', { type: 'string', demandOption: true, describe: 'Record sys_id' })
            .option('columns', { type: 'string', describe: 'Comma-separated columns (e.g. "number,short_description")' }),
          handler: wrap(async (argv, app) => {
            const params = new URLSearchParams();
            params.set('sysparm_query', `sys_id=${argv['sys-id']}`);
            params.set('sysparm_limit', '1');
            params.set('sysparm_display_value', 'true');
            if (argv.columns) params.set('sysparm_fields', argv.columns);
            const records = await app.sdk.list(argv.table, params);
            if (records.length === 0) {
              throw new Error(`Record not found: ${argv['sys-id']}`);
            }
            app.ok(records[0], { summary: `Record from ${argv.table}` });
          }),
        })
        .command({
          command: 'create',
          describe: 'Create a new record',
          builder: (y) => y
            .option('table', { type: 'string', demandOption: true, describe: 'Table name' })
            .option('data', { type: 'string', describe: 'JSON fields (e.g. \'{"state":"2"}\')' })
            .option('data-file', { type: 'string', describe: 'Read JSON payload from file' }),
          handler: wrap(async (argv, app) => {
            const recordData = parseDataArg(argv);
            const record = await app.sdk.create(argv.table, recordData);
            app.ok(record, { summary: `Created record in ${argv.table}` });
          }),
        })
        .command({
          command: 'update',
          describe: 'Update an existing record',
          builder: (y) => y
            .option('table', { type: 'string', demandOption: true, describe: 'Table name' })
            .option('sys-id', { type: 'string', demandOption: true, describe: 'Record sys_id' })
            .option('data', { type: 'string', describe: 'JSON fields (e.g. \'{"state":"2"}\')' })
            .option('data-file', { type: 'string', describe: 'Read JSON payload from file' }),
          handler: wrap(async (argv, app) => {
            const recordData = parseDataArg(argv);
            const record = await app.sdk.update(argv.table, argv['sys-id'], recordData);
            app.ok(record, { summary: `Updated record in ${argv.table}` });
          }),
        })
        .command({
          command: 'delete',
          describe: 'Delete a record',
          builder: (y) => y
            .option('table', { type: 'string', demandOption: true, describe: 'Table name' })
            .option('sys-id', { type: 'string', demandOption: true, describe: 'Record sys_id' }),
          handler: wrap(async (argv, app) => {
            await app.sdk.delete(argv.table, argv['sys-id']);
            app.ok({ message: 'Record deleted', table: argv.table, sys_id: argv['sys-id'] }, { summary: `Deleted record from ${argv.table}` });
          }),
        })

    },
    handler: () => {},
  };
}
