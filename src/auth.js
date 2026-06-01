// OAuth 2.0 with PKCE authentication
// Credentials are stored in the OS keyring (shared with Go version via libsecret/secret-tool)
// Falls back to file-based storage when keyring is unavailable.

import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import readline from 'node:readline';
import { execSync } from 'node:child_process';
import { globalConfigDir, normalizeInstanceURL } from './config.js';
import { errAuth } from './errors.js';

// ─── PKCE state persistence (shared with Go version) ───

function pkceStatePath(instance) {
  const dir = path.join(globalConfigDir(), 'pkce');
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  const filename = instance.replace(/:\/\//g, '_').replace(/\//g, '_').replace(/:/g, '_') + '.json';
  return path.join(dir, filename);
}

function savePKCEState(instance, pkce) {
  const filePath = pkceStatePath(instance);
  fs.writeFileSync(filePath, JSON.stringify(pkce, null, 2), { mode: 0o600 });
}

function loadPKCEState(instance) {
  try {
    const data = fs.readFileSync(pkceStatePath(instance), 'utf-8');
    return JSON.parse(data);
  } catch {
    return null;
  }
}

function removePKCEState(instance) {
  try {
    fs.unlinkSync(pkceStatePath(instance));
  } catch {
    // ignore
  }
}

const DEFAULT_OAUTH_CLIENT_ID = '543e5655f77746a28228c6009a599dfb';
const REDIRECT_URI = '/sdk-oauth.do';

// Keychain constants — same as Go version (internal/auth/store.go)
const KEYRING_SERVICE = 'servicenow-cli';
const KEYRING_ATTR_SERVICE = 'service';
const KEYRING_ATTR_USERNAME = 'username';

function credentialsPath(instance) {
  const dir = path.join(globalConfigDir(), 'credentials');
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  // Match Go's filename encoding: replace :// and / and : with _
  const filename = instance.replace(/:\/\//g, '_').replace(/\//g, '_').replace(/:/g, '_') + '.json';
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

// ─── Keyring via secret-tool (libsecret, same backend as Go's go-keyring) ───

function keyringLookup(instance) {
  try {
    const result = execSync(
      `secret-tool lookup ${KEYRING_ATTR_SERVICE} ${KEYRING_SERVICE} ${KEYRING_ATTR_USERNAME} "${instance}"`,
      { stdio: ['ignore', 'pipe', 'ignore'], encoding: 'utf-8' }
    );
    const trimmed = result.trim();
    if (!trimmed) return null;
    const parsed = JSON.parse(trimmed);
    // Normalize field names from Go's format to Node.js format
    return {
      auth_method: parsed.auth_method || 'oauth',
      access_token: parsed.access_token || parsed.AccessToken || '',
      refresh_token: parsed.refresh_token || parsed.RefreshToken || '',
      expires_at: parsed.expires_at || parsed.ExpiresAt || 0,
      created_at: parsed.created_at || parsed.CreatedAt || 0,
    };
  } catch {
    return null;
  }
}

function keyringStore(instance, creds) {
  try {
    execSync(
      `secret-tool store --label="Password for '${instance}' on '${KEYRING_SERVICE}'" ` +
      `${KEYRING_ATTR_SERVICE} ${KEYRING_SERVICE} ${KEYRING_ATTR_USERNAME} "${instance}"`,
      { stdio: ['pipe', 'ignore', 'ignore'], input: JSON.stringify(creds) }
    );
    return true;
  } catch {
    return false;
  }
}

function keyringDelete(instance) {
  try {
    execSync(
      `secret-tool clear ${KEYRING_ATTR_SERVICE} ${KEYRING_SERVICE} ${KEYRING_ATTR_USERNAME} "${instance}"`,
      { stdio: 'ignore' }
    );
  } catch {
    // ignore
  }
}

// ─── File-based storage (fallback) ───

function loadCredentials(instance) {
  // Try keyring first (shared with Go version)
  const keyringCreds = keyringLookup(instance);
  if (keyringCreds) return keyringCreds;

  // Fall back to file-based storage
  try {
    const data = fs.readFileSync(credentialsPath(instance), 'utf-8');
    return JSON.parse(data);
  } catch {
    return null;
  }
}

function saveCredentials(instance, creds) {
  // Try keyring first, fall back to file
  if (!keyringStore(instance, creds)) {
    fs.writeFileSync(credentialsPath(instance), JSON.stringify(creds, null, 2), { mode: 0o600 });
  }
}

function deleteCredentials(instance) {
  keyringDelete(instance);
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
    let cmd, args;
    if (platform === 'darwin') {
      cmd = 'open';
      args = [authURL];
    } else if (platform === 'win32') {
      cmd = 'cmd';
      args = ['/c', 'start', authURL];
    } else {
      cmd = 'xdg-open';
      args = [authURL];
    }
    const child = open(cmd, args, { detached: true, stdio: 'ignore' });
    child.on('error', () => {
      // Browser open command not available — user will open the URL manually
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

  /**
   * Build an OAuth authorization URL and persist PKCE state for later use.
   * After calling this, the user can visit the URL in a browser and then
   * call loginWithCode() with the resulting authorization code.
   */
  buildAuthURL(instanceURL) {
    instanceURL = normalizeInstanceURL(instanceURL);
    const clientID = getOAuthClientID();
    const pkce = generatePKCE();
    savePKCEState(instanceURL, pkce);
    return buildAuthURL(instanceURL, clientID, pkce);
  }

  /**
   * Complete login using an authorization code obtained from a prior buildAuthURL() call.
   * The PKCE state must have been saved by an earlier buildAuthURL() call.
   */
  async loginWithCode(instanceURL, code) {
    instanceURL = normalizeInstanceURL(instanceURL);
    const clientID = getOAuthClientID();
    const pkce = loadPKCEState(instanceURL);
    if (!pkce) {
      throw errAuth(
        `No pending login session for ${instanceURL}.\n\n` +
        'Run without --code first to generate one:\n' +
        `  jsn auth login ${instanceURL} --print-url\n\n` +
        'This generates the PKCE challenge and saves it. Then call:\n' +
        `  jsn auth login ${instanceURL} --code CODE`
      );
    }
    removePKCEState(instanceURL);

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
