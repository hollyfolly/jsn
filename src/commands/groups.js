import { formatRecordForDisplay, getStringField, interactiveList } from '../helpers.js';

export function groupsCmd(wrap) {
  return {
    command: 'groups [subcommand]',
    aliases: ['group'],
    describe: 'Search and display groups',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'list',
          aliases: ['ls'],
          describe: 'List groups',
          builder: (y) => y
            .option('query', { type: 'string', describe: 'Encoded query (e.g. "nameLIKEincident" or "active=true")' })
            .option('columns', { alias: 'c', type: 'string', describe: 'Comma-separated columns (e.g. "number,short_description")' })
            .option('limit', { alias: 'l', type: 'number', default: 20, describe: 'Max records' }),
          handler: wrap(async (argv, app) => {
            const columns = argv.columns ? argv.columns.split(',') : ['name', 'manager', 'email'];
            const query = argv.query || '';

            const picked = await interactiveList({
              app, table: 'sys_user_group', singular: 'group', columns, limit: argv.limit, query, labelField: 'name',
              formatLabel: r => `${getStringField(r, 'name')} ${getStringField(r, 'manager') ? '→ ' + getStringField(r, 'manager') : ''}`,
            });
            if (picked) {
              picked._context = { instance_url: app.getEffectiveInstance(), table: 'sys_user_group' };
              return app.ok(picked, { summary: `Group: ${getStringField(picked, 'name')}` });
            }

            const params = new URLSearchParams();
            params.set('sysparm_limit', String(argv.limit));
            params.set('sysparm_display_value', 'all');
            params.set('sysparm_fields', ['sys_id', ...columns].join(','));
            if (argv.query) params.set('sysparm_query', argv.query);
            const records = await app.sdk.list('sys_user_group', params);
            app.ok({
              table: 'sys_user_group',
              count: records.length,
              columns,
              records: records.map(r => formatRecordForDisplay(r, columns)),
              context: { instance_url: app.getEffectiveInstance() },
            }, { summary: `${records.length} group(s)` });
          }),
        })
        .command({
          command: 'show <name>',
          aliases: ['get'],
          describe: 'Show a group by name or sys_id',
          handler: wrap(async (argv, app) => {
            const id = argv.name;
            const isSysID = id.length === 32 && /^[0-9a-fA-F]+$/.test(id);
            const params = new URLSearchParams();
            params.set('sysparm_query', isSysID ? `sys_id=${id}` : `name=${id}`);
            params.set('sysparm_limit', '1');
            params.set('sysparm_display_value', 'all');
            const records = await app.sdk.list('sys_user_group', params);
            if (records.length === 0) {
              throw new Error(`Group not found: ${id}`);
            }
            app.ok(records[0], { summary: `Group ${id}` });
          }),
        })

    },
    handler: () => {},
  };
}
