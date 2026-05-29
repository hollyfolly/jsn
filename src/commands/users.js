import { formatRecordForDisplay, getStringField, interactiveList } from '../helpers.js';

export function usersCmd(wrap) {
  return {
    command: 'users [subcommand]',
    aliases: ['user'],
    describe: 'Search and display users',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'list',
          aliases: ['ls'],
          describe: 'List users',
          builder: (y) => y
            .option('query', { type: 'string', describe: 'Encoded query (e.g. "nameLIKEincident" or "active=true")' })
            .option('columns', { alias: 'c', type: 'string', describe: 'Comma-separated columns (e.g. "number,short_description")' })
            .option('limit', { alias: 'l', type: 'number', default: 20, describe: 'Max records' }),
          handler: wrap(async (argv, app) => {
            const columns = argv.columns ? argv.columns.split(',') : ['user_name', 'name', 'email', 'active'];
            const query = argv.query || '';

            const picked = await interactiveList({
              app, table: 'sys_user', singular: 'user', columns, limit: argv.limit, query, labelField: 'user_name',
              formatLabel: r => `${getStringField(r, 'user_name')} (${getStringField(r, 'name') || '-'})`,
            });
            if (picked) {
              picked._context = { instance_url: app.getEffectiveInstance(), table: 'sys_user' };
              return app.ok(picked, { summary: `User: ${getStringField(picked, 'user_name')}` });
            }

            const params = new URLSearchParams();
            params.set('sysparm_limit', String(argv.limit));
            params.set('sysparm_display_value', 'all');
            params.set('sysparm_fields', ['sys_id', ...columns].join(','));
            if (argv.query) params.set('sysparm_query', argv.query);
            const records = await app.sdk.list('sys_user', params);
            app.ok({
              table: 'sys_user',
              count: records.length,
              columns,
              records: records.map(r => formatRecordForDisplay(r, columns)),
              context: { instance_url: app.getEffectiveInstance() },
            }, { summary: `${records.length} user(s)` });
          }),
        })
        .command({
          command: 'show <identifier>',
          aliases: ['get'],
          describe: 'Show a user by user_name or sys_id',
          handler: wrap(async (argv, app) => {
            const id = argv.identifier;
            const isSysID = id.length === 32 && /^[0-9a-fA-F]+$/.test(id);
            const params = new URLSearchParams();
            params.set('sysparm_query', isSysID ? `sys_id=${id}` : `user_name=${id}`);
            params.set('sysparm_limit', '1');
            params.set('sysparm_display_value', 'all');
            const records = await app.sdk.list('sys_user', params);
            if (records.length === 0) {
              throw new Error(`User not found: ${id}`);
            }
            app.ok(records[0], { summary: `User ${id}` });
          }),
        })

    },
    handler: () => {},
  };
}
