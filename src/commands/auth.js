import { getEffectiveInstance, normalizeInstanceURL } from '../config.js';

export function authCmd(wrap) {
  return {
    command: 'auth <subcommand>',
    describe: 'Manage authentication',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'login [instance]',
          describe: 'Authenticate with a ServiceNow instance',
          handler: wrap(async (argv, app) => {
            const instance = argv.instance ? normalizeInstanceURL(argv.instance) : getEffectiveInstance(app.config);
            if (!instance) {
              throw new Error('No instance configured. Set via --instance or run: jsn setup');
            }
            await app.auth.login(instance);
            app.ok({ authenticated: true, instance }, { summary: `Authenticated with ${instance}` });
          }),
        })
        .command({
          command: 'logout',
          describe: 'Remove stored credentials',
          handler: wrap(async (_argv, app) => {
            const instance = getEffectiveInstance(app.config);
            if (!instance) {
              throw new Error('No instance configured');
            }
            app.auth.logout(instance);
            app.ok({ logged_out: true, instance }, { summary: `Logged out from ${instance}` });
          }),
        })
        .command({
          command: 'status',
          describe: 'Show authentication status',
          handler: wrap(async (_argv, app) => {
            const instance = getEffectiveInstance(app.config);
            if (!instance) {
              app.ok({ authenticated: false, instance: null }, { summary: 'No instance configured' });
              return;
            }
            const isAuth = app.auth.isAuthenticatedFor(instance);
            app.ok({ authenticated: isAuth, instance }, { summary: isAuth ? `Authenticated with ${instance}` : `Not authenticated with ${instance}` });
          }),
        })
        .command({
          command: 'refresh',
          describe: 'Refresh OAuth token',
          handler: wrap(async (_argv, app) => {
            const instance = getEffectiveInstance(app.config);
            if (!instance) {
              throw new Error('No instance configured');
            }
            await app.auth.refreshToken(instance, await app.auth.getCredentialsFor(instance));
            app.ok({ refreshed: true, instance }, { summary: `Token refreshed for ${instance}` });
          }),
        })

    },
    handler: () => {},
  };
}
