import { saveConfig } from '../config.js';

export function profilesCmd(wrap) {
  return {
    command: 'profiles <subcommand>',
    aliases: ['profile'],
    describe: 'Manage configuration profiles',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'list',
          describe: 'List all profiles',
          handler: wrap(async (_argv, app) => {
            const profiles = [];
            for (const [name, profile] of Object.entries(app.config.profiles || {})) {
              const instance = profile.instance_url || '';
              const isAuth = app.auth.isAuthenticatedFor(instance);
              const isDefault = name === app.config.defaultProfile;
              profiles.push({
                name,
                instance,
                username: profile.username || '',
                authenticated: isAuth,
                default: isDefault,
              });
            }
            app.ok({ profiles }, { summary: `${profiles.length} profile(s)` });
          }),
        })
        .command({
          command: 'use <name>',
          describe: 'Set active profile',
          handler: wrap(async (argv, app) => {
            const name = argv.name;
            if (!app.config.profiles[name]) {
              throw new Error(`Profile not found: ${name}`);
            }
            app.config.defaultProfile = name;
            app.config.activeProfile = name;
            saveConfig(app.config);
            app.ok({ active_profile: name }, { summary: `Active profile: ${name}` });
          }),
        })
        .command({
          command: 'show [name]',
          describe: 'Show profile details',
          handler: wrap(async (argv, app) => {
            const name = argv.name || app.config.defaultProfile || Object.keys(app.config.profiles || {})[0];
            if (!name || !app.config.profiles[name]) {
              throw new Error('No profiles configured');
            }
            const profile = app.config.profiles[name];
            app.ok({ name, ...profile }, { summary: `Profile: ${name}` });
          }),
        })
        .command({
          command: 'remove <name>',
          describe: 'Remove a profile',
          handler: wrap(async (argv, app) => {
            const name = argv.name;
            if (!app.config.profiles[name]) {
              throw new Error(`Profile not found: ${name}`);
            }
            delete app.config.profiles[name];
            if (app.config.defaultProfile === name) {
              app.config.defaultProfile = '';
            }
            if (app.config.activeProfile === name) {
              app.config.activeProfile = '';
            }
            saveConfig(app.config);
            app.ok({ removed: name }, { summary: `Removed profile: ${name}` });
          }),
        })

    },
    handler: () => {},
  };
}
