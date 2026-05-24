import { formatRecordForDisplay } from '../../helpers.js';

export function logsCmd(wrap) {
  return {
    command: 'logs [subcommand]',
    aliases: ['log'],
    describe: 'Query system logs',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'list',
          aliases: ['ls'],
          describe: 'List system logs',
          builder: (y) => y
            .option('query', { type: 'string', describe: 'Encoded query string' })
            .option('columns', { alias: 'c', type: 'string', describe: 'Comma-separated columns' })
            .option('limit', { alias: 'l', type: 'number', default: 50, describe: 'Max records' }),
          handler: wrap(async (argv, app) => {
            const columns = argv.columns ? argv.columns.split(',') : ['level', 'message', 'source', 'created'];
            const params = new URLSearchParams();
            params.set('sysparm_limit', String(argv.limit));
            params.set('sysparm_display_value', 'all');
            params.set('sysparm_fields', ['sys_id', ...columns].join(','));
            const q = argv.query ? argv.query + '^ORDERBYDESCsys_created_on' : 'ORDERBYDESCsys_created_on';
            params.set('sysparm_query', q);
            const records = await app.sdk.list('syslog', params);
            app.ok({
              table: 'syslog',
              count: records.length,
              columns,
              records: records.map(r => formatRecordForDisplay(r, columns)),
              context: { instance_url: app.getEffectiveInstance() },
            }, { summary: `${records.length} log entry(s)` });
          }),
        })
        .command({
          command: 'show <sys_id>',
          aliases: ['get'],
          describe: 'Show a log entry by sys_id',
          handler: wrap(async (argv, app) => {
            const record = await app.sdk.get('syslog', argv.sys_id);
            if (!record) {
              throw new Error(`Log entry not found: ${argv.sys_id}`);
            }
            app.ok(record, { summary: `Log entry ${argv.sys_id}` });
          }),
        });
    },
    handler: () => {},
  };
}
