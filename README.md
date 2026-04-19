# JSN - ServiceNow CLI

**Agent-first, agent-native** 

A CLI for exploring and managing ServiceNow instances. Works standalone or with any AI agent (Claude, Codex, Cursor, etc.).

```bash
# Install (or update) in seconds
curl -fsSL https://jsn.jace.pro/install | bash
```
[View install script source →](scripts/install.sh)

## Quick Start

```bash
jsn setup                           # Interactive setup (OAuth by default)
jsn tables list                     # List all tables
jsn tables schema incident          # Show incident table schema
jsn records --table incident        # List incident records
jsn rules --table incident          # Show business rules
```

---

## Why This Exists (Or: The Graveyard of ServiceNow Dev Tools)

I've been working with ServiceNow for years and trying to use tools that actually, you know, work. The Table API is "fine"—except these APIs were designed for systems integration, not for humans (or agents) trying to understand their instance.


If we want real innovation in this space, we have to stop hiding tools behind enterprise licensing agreements and convoluted setup processes. This is my attempt to build the CLI I actually want to use — and that my AI agent can use to help me.

### The Official Corpse

**[ServiceNow's "Official" CLI](https://github.com/ServiceNow/servicenow-cli)** – Last meaningful update: 2 years ago. Requires you to install a server-side application on your instance just to use it. Abandoned before it ever really lived.

### The Over-Engineered Monstrosity

**[ServiceNow Fluent SDK](https://github.com/ServiceNow/sdk)** – Follow the link rabbit hole and you eventually hit the [docs](https://www.servicenow.com/docs/r/application-development/servicenow-sdk/servicenow-sdk-landing.html). I actually tried to use this. For YEARS this thing had dependency issues that made it break on different operating systems.

Then I spent a day migrating a global scope app to a "proper" scoped app using Fluent, only to discover it made everything WORSE. Why? Because once you ship an import, you can only fix it through the SDK—not in the instance UI. Oh, and the auth configuration? Completely baffling.

### The Syncer Cemetery

Before the SDK killed them (for scoped apps only), we had file syncers. Mostly VS Code extensions that have all rotted away:

- **[sn-filesync](https://github.com/dynamicdan/sn-filesync)** – Last updated 7 years ago
- **[codesync](https://github.com/cern-snow/codesync)** – Last updated 9 years ago  
- **[now-sync](https://github.com/Accruent/now-sync)** – Last updated 6 years ago

Today there's basically one survivor: **[SNICH by Nate Anderson](https://marketplace.visualstudio.com/items?itemName=NateAnderson.snich)**—and it's VS Code only.

### The Pattern Is Clear

Every tool either:
1. Requires proprietary server-side components
2. Locks you into specific IDEs or workflows
3. Dies from complexity and maintenance burden
4. Forces you to abandon the ServiceNow UI entirely

**I'm tired of it.**

---

## How This Is Different

### 1. Actually Useful

Not "deploy a scoped app" useful—**"explore and understand your instance"** useful. The kind of tool that answers questions like:
- "What business rules fire on the Incident table?"
- "What flows are currently active?"
- "Show me the schema of this table without clicking through 12 UI screens"

### 2. Zero Bullshit Setup

One binary. No server-side plugins. No dependency hell. No auth configuration that requires a PhD.

### 3. Works With Reality

Global scope? Scoped apps? Direct instance editing? **Yes.** I'm not forcing you to choose between the CLI and the ServiceNow UI. Use both. Fix things wherever it's faster.

---

## For AI Agents

This CLI is designed to be **agent-native**. Your AI assistant can:

- **Explore**: List tables, schemas, business rules, flows — understand your instance structure
- **Query**: Fetch records, analyze data patterns, check configurations  
- **Verify**: Confirm changes, check dependencies, validate before deploying
- **Document**: Generate reports on instance configuration, security policies, automation logic

The command structure is predictable and machine-readable. JSON output available for everything.

<details>
<summary>Other installation methods</summary>

**Go install:**
```bash
go install github.com/jacebenson/jsn/cmd/jsn@latest
```

**GitHub Release:**
Download from [Releases](https://github.com/jacebenson/jsn/releases).

**From source:**
```bash
git clone https://github.com/jacebenson/jsn.git
cd jsn
go build -o jsn ./cmd/jsn/main.go
```

</details>

## Usage

```bash
# Explore your instance
jsn tables list                                    # List all tables
jsn tables schema incident                         # Show table structure
jsn tables columns incident                        # Show all columns

# Query records
jsn records --table incident                       # List records
jsn records --table incident --query "priority=1"  # Filter with encoded query
jsn records --table incident <sys_id>              # Show specific record

# Manage data
jsn records --table incident create -f short_description="Server down"
jsn records --table incident update <sys_id> -f priority=1
jsn records --table incident delete <sys_id>

# Business logic
jsn rules --table incident                         # List business rules
jsn rules --search approval                        # Search rules by name
jsn flows --active                                 # List active flows
jsn script-includes --search Utils                 # Search script includes

# Update sets
jsn updateset list                                 # List update sets
jsn updateset use <name>                           # Set current update set

# Configuration
jsn config profile                                 # Show current profile
jsn config profile <name>                          # Switch profile
jsn auth status                                    # Check auth status
```

## Updating

Re-run the install script to update to the latest version:

```bash
curl -fsSL https://jsn.jace.pro/install | bash
```

## Output Formats

```bash
jsn tables list                   # Styled output in terminal
jsn tables list --json            # JSON with envelope and breadcrumbs
jsn tables list --quiet           # Raw JSON data only
jsn tables list --md              # Markdown format
```

### JSON Envelope

Every command supports `--json` for structured output:

```json
{
  "ok": true,
  "data": [...],
  "summary": "5 tables",
  "breadcrumbs": [
    {"action": "show", "cmd": "jsn tables show incident", "description": "View table details"}
  ]
}
```

Breadcrumbs suggest next commands, making it easy for humans and agents to navigate.

## Authentication

Supports three authentication methods:

**OAuth 2.0** (recommended - most secure):
```bash
jsn auth login                     # Default - opens browser for OAuth
jsn setup                          # OAuth is the default setup method
```
Uses PKCE (Proof Key for Code Exchange) for secure token exchange. Tokens refresh automatically.

**Basic Auth** (good for CI/CD):
```bash
jsn auth login --method basic      # Enter username/password
```

**g_ck Token** (browser session):
```bash
jsn auth login --method gck        # Paste curl command from browser
```

Credentials are stored securely using your system keyring (macOS Keychain, Windows Credential Manager, Linux Secret Service). Falls back to file storage with restricted permissions if keyring is unavailable.

### Contextual Header

Every command shows your current working context:
```
# Use `jsn updateset use` or `jsn scope use` to change scope/updateset
PROFILE USER   [SCOPE]  UPDATE SET
pdi     System [global] Default
```

Each column is clickable in supporting terminals (iTerm2, Windows Terminal, GNOME Terminal 3.26+):
- **PROFILE** → Instance URL
- **USER** → Current user record
- **[SCOPE]** → Application scope
- **UPDATE SET** → Current update set

### Environment Variables

| Variable | Purpose |
|----------|---------|
| `SERVICENOW_OAUTH_TOKEN` | OAuth access token (CI/CD) |
| `SERVICENOW_OAUTH_REFRESH_TOKEN` | OAuth refresh token |
| `SERVICENOW_TOKEN` | Override stored token/password |
| `SERVICENOW_INSTANCE` | Override instance URL |
| `XDG_CONFIG_HOME` | Custom config directory |

## Configuration

```
~/.config/servicenow/         # Global configuration
├── config.json               #   Profiles and settings
└── credentials.json          #   Auth tokens (fallback when keyring unavailable)

.servicenow/                  # Per-repo configuration (optional)
└── config.json               #   Project-specific settings
```

## Discover Commands

Use `--help` to explore all commands and flags:

```bash
jsn --help                    # List all top-level commands
jsn tables --help             # Show tables subcommands
jsn records --table incident --help  # Show records flags
```

Or use `jsn` with no arguments for an interactive command picker.

## Global Flags

These flags work with any command:

```
--config <path>       # Use specific config file
--profile <name>      # Use specific profile
--json                # Output as JSON
--quiet, -q           # Output data only (no envelope)
--md                  # Output as Markdown
--agent               # Agent mode (JSON + quiet + no interactive prompts)
```

## Development

```bash
make build            # Build binary
make test             # Run Go tests
make lint             # Run linter
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup.

## License

[MIT](LICENSE)
