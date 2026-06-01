import { getEffectiveInstance, normalizeInstanceURL } from '../config.js';
import { errUsage } from '../errors.js';
import process from 'node:process';

export function authCmd(wrap) {
  return {
    command: 'auth <subcommand>',
    describe: 'Manage OAuth authentication',
    builder: (yargs) => {
      return yargs
        .command({
          command: 'login [instance]',
          describe: 'Authenticate with a ServiceNow instance via OAuth',
          builder: (sub) => sub
            .option('code', {
              describe: 'Authorization code from browser (bypasses interactive prompt)',
              type: 'string',
            })
            .option('print-url', {
              describe: 'Print the OAuth URL and exit (saves PKCE state for --code)',
              type: 'boolean',
            }),
          handler: wrap(async (argv, app) => {
            let instanceURL;
            if (argv.instance) {
              // Check if it's a profile name first
              const profiles = app.config.profiles || {};
              if (profiles[argv.instance] && profiles[argv.instance].instance_url) {
                instanceURL = normalizeInstanceURL(profiles[argv.instance].instance_url);
              } else {
                instanceURL = normalizeInstanceURL(argv.instance);
              }
            } else {
              instanceURL = getEffectiveInstance(app.config);
              if (!instanceURL) {
                // Try interactive profile picker
                const profileNames = Object.keys(app.config.profiles || {});
                if (profileNames.length > 0 && app.isInteractive()) {
                  const { search } = await import('@inquirer/prompts');
                  const choices = profileNames.map(name => ({
                    name: `${name} — ${app.config.profiles[name].instance_url}`,
                    value: name,
                  }));
                  const selected = await search({
                    message: 'Select a profile:',
                    source: (input) => {
                      const filter = (input || '').toLowerCase();
                      return choices.filter(c => c.name.toLowerCase().includes(filter));
                    },
                  });
                  instanceURL = normalizeInstanceURL(app.config.profiles[selected].instance_url);
                } else {
                  throw errUsage(`Instance URL required.

Examples:
  jsn auth login https://dev12345.service-now.com
  jsn auth login dev373698
  jsn auth login https://acme.service-now.com

Find your instance URL in your browser's address bar when logged into ServiceNow.`);
                }
              }
            }

            // --print-url: just print the URL and exit
            if (argv.printUrl) {
              const authURL = app.auth.buildAuthURL(instanceURL);
              console.log(authURL);
              return;
            }

            // --code: exchange the provided authorization code directly
            if (argv.code) {
              await app.auth.loginWithCode(instanceURL, argv.code);
            } else {
              // Interactive: full OAuth flow with browser + prompt
              await app.auth.login(instanceURL);
            }

            // Verify auth by fetching current user
            let username = '';
            try {
              if (!app.sdk) {
                const { SDKClient } = await import('../sdk.js');
                app.sdk = new SDKClient(instanceURL, app.auth);
              }
              const user = await app.sdk.getCurrentUser();
              username = user?.user_name || user?.name || '';
            } catch {
              // Non-fatal — token is saved, just couldn't verify
            }

            // Save to profiles
            const profileName = instanceURL.replace(/https?:\/\//, '').replace(/\.service-now\.com.*/, '').replace(/[^a-zA-Z0-9]/g, '-');
            if (!app.config.profiles) {
              app.config.profiles = {};
            }
            app.config.profiles[profileName] = {
              instance_url: instanceURL,
              auth_method: 'oauth',
              username: username || undefined,
            };

            // Set as default if this is the first one
            const setDefault = !app.config.instanceURL && !app.config.defaultProfile;
            if (setDefault) {
              app.config.instanceURL = instanceURL;
              app.config.defaultProfile = profileName;
            }

            const { saveConfig } = await import('../config.js');
            saveConfig(app.config);

            const result = {
              authenticated: true,
              instance: instanceURL,
              username: username || undefined,
              default: setDefault || undefined,
            };
            const summary = username
              ? `✓ Authenticated to ${instanceURL} as ${username}`
              : `✓ Authenticated to ${instanceURL}`;
            app.ok(result, { summary });
          }),
        })
        .command({
          command: 'logout [instance]',
          describe: 'Remove stored OAuth credentials',
          handler: wrap(async (argv, app) => {
            let instanceURL;
            if (argv.instance) {
              instanceURL = normalizeInstanceURL(argv.instance);
            } else {
              instanceURL = getEffectiveInstance(app.config);
              if (!instanceURL) {
                throw errUsage(`No instance specified.

Examples:
  jsn auth logout
  jsn auth logout https://dev12345.service-now.com`);
              }
            }
            app.auth.logout(instanceURL);
            app.ok({ logged_out: true, instance: instanceURL }, { summary: `✓ Logged out from ${instanceURL}` });
          }),
        })
        .command({
          command: 'status',
          describe: 'Show detailed authentication status',
          handler: wrap(async (_argv, app) => {
            const defaultInstance = getEffectiveInstance(app.config);

            // Check environment auth
            const envToken = process.env.SERVICENOW_OAUTH_TOKEN || '';

            const profiles = [];
            for (const [name, profile] of Object.entries(app.config.profiles || {})) {
              const isAuth = app.auth.isAuthenticatedFor(profile.instance_url);
              profiles.push({
                name,
                instance: profile.instance_url,
                authenticated: isAuth,
                default: profile.instance_url === defaultInstance,
              });
            }

            app.ok({
              default_instance: defaultInstance,
              authenticated: app.auth.isAuthenticated(),
              environment_auth: envToken ? true : undefined,
              profiles,
            }, { summary: `${profiles.length} profile(s)` });
          }),
        })
        .command({
          command: 'refresh [instance]',
          describe: 'Refresh OAuth token for an instance',
          handler: wrap(async (argv, app) => {
            let instanceURL;
            if (argv.instance) {
              // Check if it's a profile name or URL
              const profiles = app.config.profiles || {};
              if (profiles[argv.instance] && profiles[argv.instance].instance_url) {
                instanceURL = normalizeInstanceURL(profiles[argv.instance].instance_url);
              } else {
                instanceURL = normalizeInstanceURL(argv.instance);
              }
            } else {
              instanceURL = getEffectiveInstance(app.config);
              if (!instanceURL) {
                throw errUsage(`No instance specified and no default configured.

Examples:
  jsn auth refresh
  jsn auth refresh https://dev12345.service-now.com
  jsn auth refresh dev12345`);
              }
            }

            const creds = await app.auth.getCredentialsFor(instanceURL);
            const refreshed = await app.auth.refreshToken(instanceURL, creds);
            app.ok({
              refreshed: true,
              instance: instanceURL,
              expires_at: refreshed.expires_at,
            }, { summary: `✓ Token refreshed for ${instanceURL}` });
          }),
        });
    },
    handler: () => {},
  };
}
