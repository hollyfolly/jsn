// OAuth 2.0 with PKCE authentication

import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import readline from 'node:readline';
import { globalConfigDir, normalizeInstanceURL } from './config.js';
import { errAuth } from './errors.js';

const DEFAULT_OAUTH_CLIENT_ID = '543e5655f77746a28228c6009a599dfb';
const REDIRECT_URI = '/sdk-oauth.do';

function credentialsPath(instance) {
  const dir = path.join(globalConfigDir(), 'credentials');
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  const filename = Buffer.from(instance).toString('base64url') + '.json';
  return path.join(dir, filename);
}

function getOAuthClientID() {
  return process.env.SERVICENOW_OAUTH_CLIENT_ID || DEFAULT_OAUTH_CLIENT_ID;
}

function generatePKCE() {
  const verifier = crypto.randomBytes(32).toString('base64url');
  const challenge = crypto.createHash('sha256').update(verifier).digest('base64url');
  const state = crypto.randomBytes(16).toString('base64url');
  return { code_verifier: verifier, code_challenge: challenge, state };
}

function buildAuthURL(instanceURL, clientID, pkce) {
  const u = new URL('/oauth_auth.do', instanceURL);
  u.searchParams.set('response_type', 'code');
  u.searchParams.set('client_id', clientID);
  u.searchParams.set('redirect_uri', REDIRECT_URI);
  u.searchParams.set('state', pkce.state);
  u.searchParams.set('code_challenge', pkce.code_challenge);
  u.searchParams.set('code_challenge_method', 'S256');
  u.searchParams.set('scope', 'openid');
  return u.toString();
}

function loadCredentials(instance) {
  try {
    const data = fs.readFileSync(credentialsPath(instance), 'utf-8');
    return JSON.parse(data);
  } catch {
    return null;
  }
}

function saveCredentials(instance, creds) {
  fs.writeFileSync(credentialsPath(instance), JSON.stringify(creds, null, 2), { mode: 0o600 });
}

function deleteCredentials(instance) {
  try {
    fs.unlinkSync(credentialsPath(instance));
  } catch {
    // ignore
  }
}

function askHidden(promptText) {
  return new Promise((resolve) => {
    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
    });

    const stdin = process.stdin;
    const stdout = process.stdout;

    if (!stdin.isTTY) {
    rl.question(promptText, (answer) => {
      rl.close();
      resolve(answer.trim());
    });
    return;
  }

    stdout.write(promptText);

    stdin.setRawMode(true);
    stdin.resume();
    stdin.setEncoding('utf-8');

    let input = '';
    const onData = (key) => {
      if (key === '\r' || key === '\n') {
        stdin.removeListener('data', onData);
        stdin.setRawMode(false);
        stdin.pause();
        stdout.write('\n');
        rl.close();
        resolve(input);
      } else if (key === '\u0003') {
        process.exit();
      } else if (key === '\u007f') {
        if (input.length > 0) {
          input = input.slice(0, -1);
          stdout.write('\b \b');
        }
      } else {
        input += key;
        stdout.write('*');
      }
    };
    stdin.on('data', onData);
  });
}

export class AuthManager {
  constructor(configProvider) {
    this.configProvider = configProvider;
    this.httpClient = { timeout: 30000 };
  }

  isAuthenticated() {
    if (process.env.SERVICENOW_OAUTH_TOKEN) return true;
    const instance = this.configProvider.getEffectiveInstance();
    if (!instance) return false;
    try {
      this.getCredentialsFor(instance);
      return true;
    } catch {
      return false;
    }
  }

  isAuthenticatedFor(instance) {
    if (!instance) return false;
    const creds = loadCredentials(instance);
    if (!creds) return false;
    if (creds.expires_at && Date.now() >= creds.expires_at * 1000) return false;
    return !!creds.access_token;
  }

  async getCredentials() {
    if (process.env.SERVICENOW_OAUTH_TOKEN) {
      return { auth_method: 'oauth', access_token: process.env.SERVICENOW_OAUTH_TOKEN };
    }
    const instance = this.configProvider.getEffectiveInstance();
    if (!instance) {
      throw errAuth('No instance configured');
    }
    return this.getCredentialsFor(instance);
  }

  getCredentialsFor(instance) {
    const creds = loadCredentials(instance);
    if (!creds) {
      throw errAuth(`Not authenticated for ${instance}`);
    }
    // Check expiry — refresh if less than 5 minutes remaining
    if (creds.expires_at && Date.now() >= (creds.expires_at - 300) * 1000) {
      if (creds.refresh_token) {
        return this.refreshToken(instance, creds);
      }
      throw errAuth('Token expired, please login again');
    }
    return creds;
  }

  async login(instanceURL) {
    instanceURL = normalizeInstanceURL(instanceURL);
    const clientID = getOAuthClientID();
    const pkce = generatePKCE();
    const authURL = buildAuthURL(instanceURL, clientID, pkce);

    console.log();
    console.log('Opening browser for OAuth authentication...');
    console.log('If the browser does not open automatically, visit:');
    console.log(authURL);
    console.log();

    // Try to open browser
    const open = (await import('node:child_process')).spawn;
    const platform = process.platform;
    const cmd = platform === 'darwin' ? 'open' : platform === 'win32' ? 'start' : 'xdg-open';
    const child = open(cmd, [authURL], { detached: true, stdio: 'ignore' });
    child.on('error', () => {
      // xdg-open not installed — user will open the URL manually
    });
    child.unref();

    console.log('After authenticating in the browser, copy the authorization code shown on the page.');
    console.log('(input is hidden for security — just paste and press Enter)');
    console.log();

    const authCode = await askHidden('Authorization code (hidden on paste for security): ');
    const code = authCode.trim();
    if (!code) {
      throw errAuth('Authorization code is required');
    }

    console.log('\nExchanging authorization code for tokens...');
    const newCreds = await this.exchangeCode(instanceURL, clientID, code, pkce);
    saveCredentials(instanceURL, newCreds);
    return newCreds;
  }

  async exchangeCode(instanceURL, clientID, code, pkce) {
    const tokenURL = `${instanceURL.replace(/\/$/, '')}/oauth_token.do`;
    const body = new URLSearchParams();
    body.set('grant_type', 'authorization_code');
    body.set('client_id', clientID);
    body.set('code', code);
    body.set('redirect_uri', REDIRECT_URI);
    body.set('code_verifier', pkce.code_verifier);

    const resp = await fetch(tokenURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });

    const text = await resp.text();
    if (!resp.ok) {
      throw errAuth(`Token exchange failed (status ${resp.status}): ${text}`);
    }

    const tokenResp = JSON.parse(text);
    const expiresAt = tokenResp.expires_in ? Math.floor(Date.now() / 1000) + tokenResp.expires_in : 0;
    return {
      auth_method: 'oauth',
      access_token: tokenResp.access_token,
      refresh_token: tokenResp.refresh_token,
      expires_at: expiresAt,
      created_at: Math.floor(Date.now() / 1000),
    };
  }

  async refreshToken(instance, creds) {
    const tokenURL = `${instance.replace(/\/$/, '')}/oauth_token.do`;
    const clientID = getOAuthClientID();
    const body = new URLSearchParams();
    body.set('grant_type', 'refresh_token');
    body.set('client_id', clientID);
    body.set('refresh_token', creds.refresh_token);

    const resp = await fetch(tokenURL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });

    if (!resp.ok) {
      const text = await resp.text();
      throw errAuth(`Token refresh failed: ${text}`);
    }

    const tokenResp = await resp.json();
    const newCreds = {
      auth_method: 'oauth',
      access_token: tokenResp.access_token,
      refresh_token: tokenResp.refresh_token,
      created_at: Math.floor(Date.now() / 1000),
    };
    if (tokenResp.expires_in) {
      newCreds.expires_at = Math.floor(Date.now() / 1000) + tokenResp.expires_in;
    }
    saveCredentials(instance, newCreds);
    return newCreds;
  }

  logout(instance) {
    if (!instance) {
      throw errAuth('No instance specified');
    }
    deleteCredentials(instance);
  }
}
