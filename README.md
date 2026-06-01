# JSN - ServiceNow CLI

A command-line interface for ServiceNow that follows the Unix philosophy: simple, composable, and scriptable.

## Installation

### npm (recommended — cross-platform)

```bash
npm install -g @jacebenson/jsn
```

Works on macOS, Linux, and Windows with **Node.js 18+**. No compilation needed.

Verify the installation:

```bash
jsn --version
jsn --help
```

### Quick Start

```bash
# 1. Set up your first ServiceNow instance
jsn setup

# 2. Verify authentication
jsn auth status

# 3. Start using it
jsn incidents
jsn changes
jsn requests
```

That's it. No config files to write, no binaries to download.

## Commands

### Core

| Command | Aliases | Description |
|---------|---------|-------------|
| `incidents` | `incident`, `inc` | Manage IT incidents |
| `changes` | `change`, `chg` | Manage change requests |
| `requests` | `request`, `req` | Manage service catalog requests |
| `tasks` | `task`, `sctask` | Manage service catalog tasks |
| `tickets` | `ticket` | Query generic tickets |

### Data & Admin

| Command | Aliases | Description |
|---------|---------|-------------|
| `records` | — | Generic Table API for any table |
| `users` | `user` | Manage ServiceNow users |
| `groups` | `group` | Manage groups |
| `groupmembers` | `gm` | Manage group memberships |
| `grouproles` | `gr` | Manage group roles |

### Development

| Command | Description |
|---------|-------------|
| `dev` | Manage development artifacts (flows, includes, rules, ACLs, scopes, etc.) |

### Configuration

| Command | Aliases | Description |
|---------|---------|-------------|
| `setup` | — | Interactive first-time setup |
| `auth` | — | Manage OAuth authentication |
| `profiles` | `profile` | Manage instance profiles |

### Utility

| Command | Description |
|---------|-------------|
| `skill` | Manage the AI agent skill file (show, fetch, install) |
| `version` | Show version; use `--check` to check npm for updates |

## Usage Examples

### Incidents

```bash
# List all incidents
jsn incidents

# List critical incidents
jsn incidents list --query "priority=1"

# Show specific incident
jsn incidents INC0010001

# Create incident
jsn incidents create --description "Server down" --priority 1

# Update incident
jsn incidents update INC0010001 --data '{"state": "6"}'

# Delete incident
jsn incidents delete INC0010001
```

### Changes

```bash
# List all change requests
jsn changes

# Create change request
jsn changes create --description "Deploy feature" --risk medium

# Update change
jsn changes update CHG0010001 --data '{"state": "3"}'
```

### Requests (Catalog Items)

```bash
# List service catalog requests
jsn requests

# Show with attachments and catalog variables
jsn requests show RITM0010001
```

The `requests show` command enriches the record with attachments and catalog variables automatically.

### Users & Groups

```bash
# Search users
jsn users "John Smith"

# Create a user
jsn users create --user-name john.smith --name "John Smith" --email john@example.com

# Delete a user
jsn users delete john.smith

# List groups
jsn groups

# Create a group
jsn groups create --name "Engineering" --description "Engineering team"

# Add a user to a group
jsn groupmembers add --user john.smith --group "Engineering"
```

### Generic Table API

```bash
# List any table
jsn records list --table incident --limit 10

# Query with encoded query
jsn records list --table incident --query "priority=1^active=true"

# Get a record by sys_id
jsn records get --table incident --sys-id abc123

# Create a record
jsn records create --table incident --data '{"short_description": "Test"}'
```

### Development Artifacts

```bash
# Automations
jsn dev flows               # List Flow Designer flows
jsn dev actions             # List action definitions

# Scripts
jsn dev includes            # List script includes
jsn dev rules               # List business rules
jsn dev clientscripts       # List client scripts
jsn dev uiactions           # List UI actions
jsn dev uipolicies          # List UI policies

# Security
jsn dev acls                # List access controls
jsn dev roles               # List roles

# Platform
jsn dev updatesets          # List update sets
jsn dev scopes              # List application scopes
jsn dev properties          # List system properties
jsn dev logs                # Query system logs
jsn dev eval                # Execute background scripts

# Data
jsn dev tables incident     # View table definition
jsn dev columns --table incident  # List columns
```

## AI Agent Support

jsn ships with a skill file for AI agent compatibility (Hermes, Claude Code, Cursor, OpenCode, etc.):

```bash
# Show the bundled skill file
jsn skill show

# Download the latest from GitHub
jsn skill fetch

# Download and install to the Hermes skills directory
jsn skill install

# View all skill-related file paths
jsn skill path
```

This skill file teaches AI agents how to use jsn safely and effectively.

## Checking for Updates

```bash
jsn version --check
```

If a newer version is available, you'll see:

```
jsn 0.1.0 — newer version 0.2.0 available
  → update: npm install -g @jacebenson/jsn
```

## Configuration

JSN uses a layered configuration system:

| Source | Priority | Description |
|--------|----------|-------------|
| Flags | Highest | `--instance`, `--profile`, `--format` |
| Environment | High | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_FORMAT` |
| Local config | Medium | `./.servicenow/config.json` |
| Global config | Low | `~/.config/servicenow/config.json` |
| Defaults | Lowest | Built-in defaults |

### Profiles

Work with multiple ServiceNow instances:

```bash
jsn auth login https://dev12345.service-now.com    # Login and auto-create profile
jsn profiles list                                   # List all profiles
jsn profiles use dev12345                           # Switch profile
jsn profiles show                                   # Show current profile
```

## Output Formats

| Format | Flag | Description |
|--------|------|-------------|
| Auto (default) | `--format=auto` | JSON for pipes, styled for TTY |
| JSON | `--json` or `--format=json` | Machine-readable JSON |
| Styled | `--styled` | ANSI-styled tables (for humans) |
| Markdown | `--markdown` | Markdown tables |
| Quiet | `--quiet` or `-q` | Data only, no envelope |

```bash
jsn incidents --json        # JSON output
jsn incidents --styled      # Styled table output
jsn incidents --markdown    # Markdown tables
jsn incidents -q | jq '.'   # Quiet mode for piping
```

## Authentication

JSN uses OAuth 2.0 with PKCE for secure authentication:

```bash
jsn auth login https://dev12345.service-now.com     # Interactive browser login
jsn auth status                                      # Check authentication
jsn auth refresh                                     # Refresh token
jsn auth logout                                      # Logout
```

Credentials are stored in `~/.config/servicenow/credentials/` (file-based, with OS keyring support).

For CI/CD and bot environments, use the `--code` and `--print-url` flags:

```bash
# Generate auth URL manually (no browser auto-open)
jsn auth login https://dev.service-now.com --print-url

# Complete auth with authorization code
jsn auth login https://dev.service-now.com --code "eyJ..."
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `SERVICENOW_INSTANCE_URL` | Default instance URL |
| `SERVICENOW_FORMAT` | Default output format |
| `SERVICENOW_OAUTH_CLIENT_ID` | Custom OAuth client ID |
| `SERVICENOW_OAUTH_TOKEN` | OAuth token (for CI/CD) |

## CI/CD Integration

```bash
export SERVICENOW_INSTANCE_URL="https://dev12345.service-now.com"
export SERVICENOW_OAUTH_TOKEN="***"

# Now run commands without interactive auth
jsn incidents list
```

## Getting Help

```bash
jsn --help                    # Grouped command list
jsn <command> --help          # Command-specific help with flags
jsn <command> list --help     # Subcommand options
```

## Shell Completion

Shell completion is available. To install:

```bash
# Bash — add to your .bashrc
source <(jsn completion bash)

# Zsh — add to your .zshrc
source <(jsn completion zsh)

# Fish
jsn completion fish | source
```

## Development

### Setup

```bash
git clone https://github.com/jacebenson/jsn
cd jsn
git checkout nodejs
npm install
npm test           # Run tests (128+ tests)
npm run lint       # Run ESLint
npm start -- --help  # Test locally
```

### Releasing

```bash
# Publish to npm
npm run release -- patch    # patch bump
npm run release -- minor    # minor bump
npm run release -- major    # major bump
```

The Release GitHub Action will automatically build, tag, and publish to npm on push.

## Previous Versions (Go)

jsn was originally written in Go. The Node.js version on the `nodejs` branch is now at full feature parity. The Go version lives on the `main` branch and is considered stable but no longer active.

| Version | Language | Branch | Install | Status |
|---------|----------|--------|---------|--------|
| Node.js | JavaScript (Node 18+) | `nodejs` | `npm install -g @jacebenson/jsn` | ✅ Active |
| Go | Go | `main` | Binary download | ⏸️ Stable (legacy) |

To install the Go version (legacy):

```bash
curl -L https://github.com/jacebenson/jsn/releases/latest/download/jsn-linux-amd64 -o jsn
chmod +x jsn && sudo mv jsn /usr/local/bin/
```

## Contributing

1. Fork the repository
2. Create a feature branch (target `nodejs` for new features, `main` for Go fixes)
3. Make your changes with tests
4. Submit a pull request

Development commands:

```bash
npm test       # Node.js test suite (128+ tests)
npm run lint   # ESLint
npm start      # Run locally
```

## License

MIT License — see [LICENSE](LICENSE) for details.

## Acknowledgments

This project follows the architectural patterns from [basecamp-cli](https://github.com/basecamp/basecamp-cli).

**Inspiration and references:**

| Project | What we learned |
|---------|----------------|
| [Abey's `sn` CLI](https://github.com/tehubersheezy/servicenow-cli) (Rust) | Table API patterns, aggregate stats, raw REST passthrough |
| [ServiceNow `now-sdk`](https://www.npmjs.com/package/@servicenow/sdk) | OAuth session flow using `angular.do?sysparm_type=get_user`, `X-UserToken` header for UI pages |
| [ServiceNow VS Code Extension](https://marketplace.visualstudio.com/items?itemName=ServiceNow.now-vscode) | `sys.scripts.do` CSRF extraction pattern for background script execution |
| [Getting Real](https://basecamp.com/gettingreal) by Basecamp | Build less, start with no, embrace constraints |
