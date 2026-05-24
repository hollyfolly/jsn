import readline from 'node:readline';
import { getEffectiveInstance, normalizeInstanceURL, setProfile } from '../config.js';

export function setupCmd(wrap) {
  return {
    command: 'setup',
    describe: 'Interactive first-time setup',
    handler: wrap(async (_argv, app) => {
      const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
      const ask = (q) => new Promise((resolve) => rl.question(q, resolve));

      console.log('Welcome to JSN - ServiceNow CLI');
      console.log();

      let instance = getEffectiveInstance(app.config);
      if (!instance) {
        instance = await ask('ServiceNow instance URL (e.g., dev12345.service-now.com): ');
        instance = normalizeInstanceURL(instance);
      }
      console.log(`Instance: ${instance}`);

      const profileName = await ask('Profile name (default): ') || 'default';
      await setProfile(app.config, profileName, { instance_url: instance });

      const loginNow = await ask('Login now? [Y/n]: ');
      if (!loginNow || loginNow.toLowerCase() !== 'n') {
        await app.auth.login(instance);
        console.log('Login successful!');
      }

      rl.close();
      app.ok({ setup: true, instance, profile: profileName }, { summary: 'Setup complete' });
    }),
  };
}
