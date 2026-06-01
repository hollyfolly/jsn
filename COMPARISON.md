# JSN — Go → Node.js Migration Delta

> Generated: 2026-06-01
> Comparison between `main` (Go) and `nodejs` (Node.js) branches
> Branch: `comparison/golang-migration-delta`

## Overview

Both versions share **~90% feature parity** on the CLI surface — same command structure, same dev subcommands, same config paths, same credential storage, same OAuth PKCE flow, same output envelope format, same error codes.

The Node.js version is a complete port of the Go version's functionality. All Go commands have Node.js equivalents. The gaps are in implementation depth, not feature absence.

**Architecture comparison:**

| Dimension | Go (main) | Node.js (nodejs) |
|-----------|-----------|-------------------|
| Language | Go 1.25.0 | Node.js 18+ |
| CLI framework | Cobra (spf13/cobra) | yargs (17.x) |
| Config | JSON files + XDG | JSON files + XDG (identical paths) |
| Auth | OAuth 2.0 + PKCE + keyring | OAuth 2.0 + PKCE + keyring |
| Output envelope | Structured `{ok, data, error, ...}` | Structured `{ok, data, error, ...}` |
| TUI framework | Bubble Tea (charmbracelet) | @inquirer/search (prompts) |
| Test files | **12** test files | **2** test files |
| Dependencies | 85+ indirect via gojq, bubbletea, etc. | yargs, chalk, inquirer, cli-table3 |

---

## 1. Command Surface Area

### 1.1 Work Commands — Fully Equivalent

| Command | Go Flags/Aliases | Node Flags/Aliases | Status |
|---------|-----------------|-------------------|--------|
| `jsn setup` | interactive wizard | interactive wizard | ✅ Matching |
| `jsn auth login` | `--code`, `--print-url` | `--code`, `--print-url` | ✅ Matching |
| `jsn auth refresh` | — | — | ✅ Matching |
| `jsn auth logout` | — | — | ✅ Matching |
| `jsn auth status` | — | — | ✅ Matching |
| `jsn profiles list/show/use/remove` | full CRUD | full CRUD | ✅ Matching |
| `jsn records list/get/create/update/delete` | full CRUD via `--table` | full CRUD via `--table` | ✅ Matching |
| `jsn incidents [list/show/create/update/delete]` | `-d, --priority`, `-c, --columns`, `-l, --limit`, `-o, --offset` | `-d, --priority`, `-c, --columns`, `-l, --limit`, `-o, --offset` | ✅ Matching |
| `jsn changes [list/show/create/update/delete]` | same | same | ✅ Matching |
| `jsn requests [list/show]` | no create/update/delete | no create/update/delete | ✅ Matching |
| `jsn tasks [list/show]` | no create/update/delete | no create/update/delete | ✅ Matching |
| `jsn tickets [list/show/create/update/delete]` | full CRUD | full CRUD | ✅ Matching |
| `jsn version` | semver + update check | semver + update check | ✅ Matching |

### 1.2 Work Commands — Node.js Incomplete 🔴

| Command | Go has | Node has | Gap |
|---------|--------|----------|-----|
| `jsn users` | **list, show, create, update, delete** | list, show only | 🔴 Missing: `create`, `update`, `delete` |
| `jsn groups` | **list, show, create, update, delete** | list, show only | 🔴 Missing: `create`, `update`, `delete` |
| `jsn groupmembers` | **list, add, remove** | list only | 🔴 Missing: `add`, `remove` |
| `jsn grouproles` | **list, add, remove** | list only | 🔴 Missing: `add`, `remove` |

### 1.3 Dev Subcommands — Mostly Equivalent

All 25 dev subcommands exist in both versions. The Go version has a richer TUI for interactive picking and more detailed show output. Specific differences:

| Dev Command | Go Details | Node Details | Status |
|-------------|-----------|-------------|--------|
| `dev flows` | list, show, **create, update, delete** | list, show only | 🔴 Missing CRUD |
| `dev includes` | full CRUD | full CRUD | ✅ |
| `dev rules` | full CRUD | full CRUD | ✅ |
| `dev clientscripts` | full CRUD | full CRUD | ✅ |
| `dev uiactions` | full CRUD | full CRUD | ✅ |
| `dev uipolicies` | full CRUD | full CRUD | ✅ |
| `dev actions` | full CRUD | full CRUD | ✅ |
| `dev updatesets` | list, show, set | list, show, set, **create** | 🟢 Node has extra: `create` |
| `dev scopes` | list, show, create | list, show, create, **set** | 🟢 Node has extra: `set` |
| `dev eval` | `--script`, `--file` | `--script`, `--file` | ✅ |
| `dev rest` | `--method`, `--data`, `--table`, `--query` | `--method`, `--data`, `--table`, `--query` | ✅ |
| `dev logs` | list, show | list, show | ✅ |
| `dev forms` | list (by table), show (by table + view) | list (by table), show (by table + view) | ✅ |
| `dev lists` | list (by table), show (by table + view) | list (by table), show (by table + view) | ✅ |
| `dev tables/columns/import/acls/roles/properties/sppages/spwidgets/uipages/appmenu/scrapi` | mixed read-only or CRUD | mixed read-only or CRUD (matching) | ✅ |

---

## 2. Auth System

### 2.1 Feature Comparison

| Feature | Go | Node.js | Notes |
|---------|----|---------|-------|
| OAuth 2.0 + PKCE | ✅ | ✅ | Same SHA256-32-byte algorithm |
| Interactive browser open | ✅ `xdg-open`/`open`/`rundll32` | ✅ `open` npm package | Same behavior |
| `--print-url` mode | ✅ Saves PKCE to `~/.config/servicenow/pkce/` | ✅ Same | Cross-compatible format |
| `--code` exchange with saved PKCE | ✅ Deletes state after use | ✅ Same | Cross-compatible |
| Token refresh (auto/manual) | ✅ | ✅ | Same `grant_type=refresh_token` |
| Keyring integration | ✅ `zalando/go-keyring` | ✅ `keytar`-style via file? | **Needs verification** |
| File credential fallback | ✅ `~/.config/servicenow/credentials/` | ✅ Same | Cross-compatible format |
| `SERVICENOW_OAUTH_TOKEN` env var | ✅ Skips all storage | ❓ | **Needs verification** |
| `SERVICENOW_OAUTH_CLIENT_ID` env var | ✅ Overridable | ❓ | **Needs verification** |
| `approval_prompt=force` in auth URL | ✅ Forces re-consent each login | ❓ | **Needs verification** |
| Platform-specific browser code | ✅ Build tags (3 files) | ✅ `process.platform` switch | |
| Hidden password input | ✅ `x/term.ReadPassword` | ✅ `readline` raw mode | |
| Default OAuth client ID | `543e5655f77746a28228c6009a599dfb` | Same | ServiceNow SDK client ID |

### 2.2 Detailed Auth Flow (both versions)

```
1. Generate PKCE: 32-byte random verifier → SHA256 → base64url challenge
2. Generate 16-byte random state
3. Build URL: /oauth_auth.do?response_type=code&client_id=...&code_challenge=...&state=...&scope=openid
4. Either open browser or print URL (--print-url saves PKCE state to disk)
5. User authorizes → redirected to /sdk-oauth.do?code=...
6. Exchange POST /oauth_token.do with grant_type=authorization_code + code_verifier
7. Save credentials to keyring (preferred) or ~/.config/servicenow/credentials/<instance>.json
```

---

## 3. SDK / API Client

### 3.1 Methods Available

| SDK Method | Go | Node.js | Notes |
|-----------|----|---------|-------|
| `list(table, params)` | ✅ | ✅ | Same paginated query |
| `get(table, sysID)` | ✅ | ✅ | |
| `create(table, data)` | ✅ | ✅ | |
| `update(table, sysID, data)` | ✅ | ✅ | |
| `delete(table, sysID)` | ✅ | ✅ | |
| `getCurrentUser()` | ✅ | ✅ | Same: `sys_user` with `javascript:gs.getUserID()` |
| `aggregateCount(table, query)` | ✅ | ✅ | Same `GET /api/now/stats/{table}?sysparm_count=true` |
| `executeScript(script)` | ✅ | ✅ | Same: warm session → scrape CSRF → POST `/sys.scripts.do` → parse `<PRE>` |
| `inspectFlow(identifier)` | ✅ | ✅ | Same: resolve by sys_id or name, parse payload JSON, fetch sub-tables |
| `RecordWithRelated(table, query, related)` | ✅ | ❌ | 🔴 Parallel fetch of related tables |
| `FetchAttachments(tableName, sysID)` | ✅ | ❌ | 🔴 Query `sys_attachment` |
| `FetchCatalogVariables(ritmSysID)` | ✅ | ❌ | 🔴 Query `sc_item_option` + variables |
| `GetCurrentUpdateSet(userID)` | ✅ | ❌ | 🔴 Two-step via `sys_user_preference` |
| `GetCurrentApplication(userID)` | ✅ | ❌ | 🔴 Two-step via `sys_user_preference` |

### 3.2 Script Execution Details

Both versions implement the same hack for background script execution:
1. Create dedicated HTTP client with cookie jar
2. Warm session by hitting REST API with auth
3. GET `/sys.scripts.do`, scrape `sysparm_ck` from hidden `<input>`
4. POST to`/sys.scripts.do` with form data: `script`, `sysparm_ck`, `runscript`, etc.
5. Parse `<PRE>` tags from HTML response, convert `<BR>` to newlines

---

## 4. Config System

### 4.1 Config Loading Chain

| Layer | Go | Node.js | Notes |
|-------|----|---------|-------|
| Defaults | `Format: auto` | `Format: auto` | ✅ |
| Global config | `~/.config/servicenow/config.json` | `~/.config/servicenow/config.json` | ✅ Same |
| Local config | `./.servicenow/config.json` | `./.servicenow/config.json` | ✅ Same |
| Env vars | `SERVICENOW_INSTANCE_URL`, `SERVICENOW_FORMAT` | Same | ✅ Same |
| Flag overrides | `--instance`, `--profile`, `--format` | Same | ✅ Same |

### 4.2 Config File Format

```json
{
  "instance_url": "https://dev12345.service-now.com",
  "format": "auto",
  "default_profile": "prod",
  "profiles": {
    "dev": { "instance_url": "https://dev12345.service-now.com" },
    "prod": { "instance_url": "https://prod.service-now.com" }
  }
}
```

**Cross-compatible?** Both versions write and read the same format. ✅

### 4.3 Credential Storage

| Mechanism | Go | Node.js | Cross-compatible? |
|-----------|----|---------|-------------------|
| Keyring | ✅ `secret-service`/`keychain` | ✅ Similar | Needs testing |
| File fallback | `~/.config/servicenow/credentials/<instance>.json` | Same format | ✅ Yes (verified) |
| PKCE state | `~/.config/servicenow/pkce/<instance>.json` | Same format | ✅ Yes |

---

## 5. Output Formatting

### 5.1 Envelope Structure

```json
{
  "ok": true,
  "data": [...],
  "summary": "12 records loaded",
  "breadcrumbs": [
    { "action": "list", "cmd": "jsn incidents list", "description": "List all incidents" }
  ],
  "notice": "...",
  "context": { "profile": "dev", "instance": "..." },
  "meta": { "count": 12, "time_ms": 234 }
}
```

Both versions output the exact same envelope format. ✅

### 5.2 Error Envelope

```json
{
  "ok": false,
  "error": "Not authenticated",
  "code": "auth_error",
  "hint": "Run 'jsn auth login' to authenticate"
}
```

| Error Code | Go | Node.js |
|-----------|----|---------|
| `usage_error` | ✅ | ✅ |
| `not_found` | ✅ | ✅ |
| `auth_error` | ✅ | ✅ |
| `forbidden` | ✅ | ✅ |
| `rate_limited` | ✅ | ✅ |
| `network_error` | ✅ | ✅ |
| `api_error` | ✅ | ✅ |
| `ambiguous` | ✅ | ✅ |
| `empty_result` | ✅ | ✅ |

### 5.3 Output Formats

| Format | Go | Node.js | Notes |
|--------|----|---------|-------|
| `auto` (TTY=styled, pipe=json) | ✅ | ✅ | Same |
| `json` | ✅ | ✅ | Same envelope |
| `markdown` | ✅ | ✅ | Tables for arrays |
| `styled` (ANSI) | ✅ | ✅ | **Different implementations** |
| `quiet` (data only) | ✅ | ✅ | Same |
| jq filtering on output | ✅ (`gojq` native) | ❌ Only shell piping | 🔴 Go has built-in jq |

---

## 6. Interactive TUI / Picking

| Feature | Go | Node.js |
|---------|----|---------|
| Framework | Bubble Tea (Elm architecture) | @inquirer/search |
| Search-as-you-type | ✅ Server-side query after 2+ chars | ✅ Same |
| Paginated loading | ✅ Auto-loads more as user scrolls | ✅ Same |
| Emoji/color icons | ✅ Priority emojis, colored status | ✅ Similar |
| Profile picker | ✅ Custom picker | ✅ Yargs prompts |
| Update set picker | ✅ Custom picker w/ state icons | ✅ List-based |
| Type-to-jump (local filter) | ✅ 1-char local filter | ✅ Via inquirer |
| Keyboard navigation | ✅ Full vim/arrow support | ✅ Arrow keys |
| `ListInteractive(table, pageFetcher)` | ✅ One-shot function for commands | ✅ Via `_ticket.js` builder |

**Key difference**: Go's Bubble Tea provides a richer terminal experience with color theming, custom layouts, and emoji support. Node's `@inquirer/search` is simpler but functionally equivalent for search-and-pick UX.

---

## 7. Tests

| Metric | Go | Node.js |
|--------|----|---------|
| Test files | **12** (`*_test.go`) | **2** (`*.test.js`) |
| Total lines | ~2,000+ | 150 |
| Command-specific tests | ✅ Per-command files | ❌ 2 generic files |
| Mock transport | ✅ Custom HTTP mock | ❌ Network calls directly |
| Unit tests | ✅ | ✅ Config + auth |

The Go version has significantly better test coverage with per-command test files and mock HTTP transport. The Node.js version has only a config unit test and a smoke test that makes real API calls.

---

## 8. Detailed File-by-File Comparison

### 8.1 Commands

| File | Go (`internal/commands/`) | Node (`src/commands/`) | Delta |
|------|--------------------------|----------------------|-------|
| `auth.go` | 402 lines | 274 lines | Go has richer error messages and PKCE mismatch explanations |
| `incidents.go` | 116 lines | 41 lines (via `_ticket.js`) | Node uses builder pattern |
| `changes.go` | 96 lines | 41 lines (via `_ticket.js`) | Node uses builder pattern |
| `requests.go` | 90 lines | 41 lines (via `_ticket.js`) | Node uses builder pattern |
| `tasks.go` | 90 lines | 41 lines (via `_ticket.js`) | Node uses builder pattern |
| `tickets.go` | 105 lines | 41 lines (via `_ticket.js`) | Node uses builder pattern |
| `users.go` | 112 lines | 63 lines | 🔴 Node missing create/update/delete |
| `groups.go` | 112 lines | 63 lines | 🔴 Node missing create/update/delete |
| `groupmembers.go` | 95 lines | 31 lines | 🔴 Node missing add/remove |
| `grouproles.go` | 95 lines | 31 lines | 🔴 Node missing add/remove |
| `records.go` | 185 lines | 198 lines | ✅ Roughly equivalent |
| `profiles.go` | 165 lines | 176 lines | ✅ Roughly equivalent |
| `setup.go` | 90 lines | 82 lines | ✅ Same wizard-flow |
| `version.go` | 20 lines | 13 lines | ✅ Equivalent |
| `dev.go` + subcommands | 25 files | 1 file + builders | Node uses `_generic.js` + `_simple.js` |

### 8.2 SDK

| File | Go (`internal/sdk/`) | Node (`src/sdk.js`) | Delta |
|------|---------------------|--------------------|-------|
| `client.go` | 688 lines | 540 lines | Go has RecordWithRelated, FetchAttachments, FetchCatalogVariables, GetCurrentUpdateSet, GetCurrentApplication |
| `helpers.go` | 434 lines | N/A (inline) | Go has helper functions extracted separately |
| `context.go` | 154 lines | N/A (in app.js) | Context plumbing |

### 8.3 Infrastructure

| File | Go | Node | Delta |
|------|----|------|-------|
| CLI entry | `cmd/jsn/main.go` (8 lines) | `bin/jsn.js` (31 lines) | Both thin wrappers |
| Root CLI | `internal/cli/root.go` (211 lines) | `src/cli.js` (305 lines) | Both set up global flags and middleware |
| App context | `internal/appctx/app.go` (243 lines) | `src/app.js` (84 lines) + `src/context.js` (42 lines) | Go has richer PrintContextHeader |
| Config | `internal/config/config.go` (268 lines) | `src/config.js` (226 lines) | ✅ Same layering |
| Auth | `internal/auth/auth.go` (480 lines) + `store.go` (121 lines) | `src/auth.js` (286 lines) | Go has more extensive store management |
| Output | `internal/output/envelope.go` (978 lines) + `errors.go` (149 lines) | `src/output.js` (260 lines) + `src/errors.js` (63 lines) | Go has much richer output formatting (breadcrumbs, context, grouping) |
| TUI | `internal/tui/` (5 files, ~1,618 lines) | Via `@inquirer/prompts` and `@inquirer/search` | Different frameworks, same UX |

---

## 9. Migration Priority

### Phase 1: Feature Parity (Low Hanging Fruit) 🎯

These bring the Node.js version to full feature parity with minimal code:

| Priority | Gap | Files to modify | Complexity |
|----------|-----|----------------|------------|
| 🔴 P0 | `users` — missing create/update/delete | `src/commands/users.js` | Easy — replicate from `_ticket.js` pattern |
| 🔴 P0 | `groups` — missing create/update/delete | `src/commands/groups.js` | Easy — replicate from `_ticket.js` pattern |
| 🔴 P0 | `groupmembers` — missing add/remove | `src/commands/groupmembers.js` | Easy — 2 additional subcommands |
| 🔴 P0 | `grouproles` — missing add/remove | `src/commands/grouproles.js` | Easy — 2 additional subcommands |
| 🟡 P1 | `dev flows` — missing create/update/delete | `src/commands/dev/flows.js` | Medium — needs flow-specific API calls |
| 🟢 P2 | `dev updatesets` — `create` exists in Node only | Keep; add to Go? | Trivial |

### Phase 2: SDK Enrichment 🔧

| Priority | Gap | Files to modify | Complexity |
|----------|-----|----------------|------------|
| 🟡 P1 | `getCurrentUpdateSet` | `src/sdk.js` | Medium — two-step via `sys_user_preference` |
| 🟡 P1 | `getCurrentApplication` | `src/sdk.js` | Medium — same pattern |
| 🟡 P1 | `fetchAttachments` | `src/sdk.js` | Easy — simple query to `sys_attachment` |
| 🟡 P1 | `fetchCatalogVariables` | `src/sdk.js` | Medium — two-level reference resolution |
| 🔵 P3 | `recordWithRelated` | `src/sdk.js` | Medium — concurrent Promise.all fetches |

### Phase 3: Quality & Polish ✨

| Priority | Gap | Complexity |
|----------|-----|------------|
| 🟡 P1 | Node.js env var support: `SERVICENOW_OAUTH_CLIENT_ID`, `SERVICENOW_OAUTH_TOKEN` | Easy |
| 🟡 P1 | `approval_prompt=force` in auth URL | Trivial |
| 🟡 P1 | Test coverage — add per-command tests with mock transport | Medium |
| 🔵 P3 | jq-style built-in filtering on output | Medium |
| 🔵 P3 | `PrintContextHeader` — scope + update set display at top of interactive sessions | Medium |
| 🔵 P3 | Richer formatted record display (field grouping, related ACL roles) | Medium |

---

## 10. Technical Debt & Architecture Notes

### 10.1 Dead Code / Unused Files

- **`internal/commands/dev/` in Go**: Has ~18 individual `.go` files, each for a specific table. Nodes uses 2 generic builders (`_generic.js`, `_simple.js`) for the same purpose. The Go pattern is more verbose but more flexible per-table.
- **`src/commands/dev/` in Node**: Has individual `flows.js`, `forms.js`, `lists.js`, `logs.js`, `updatesets.js`, `scopes.js`, `eval.js`, `rest.js` alongside `_generic.js` and `_simple.js`. This is a mixed approach — some dev commands use the builder, some have custom implementations.

### 10.2 Builder Pattern Comparison

**Go dev commands**: Each table gets its own cobra command file (18 files in `internal/commands/dev/`). The `BuildDevCmd` pattern is used but less consistently than Node's approach.

**Node dev commands**: Uses two builders:
- `_generic.js` (`buildDevCmd`) — for commands with custom subcommand logic (flows, forms, lists, logs, updatesets, scopes, eval, rest)
- `_simple.js` (`buildSimpleDevCmd`) — for simple table CRUD (includes, rules, clientscripts, uiactions, uipolicies, actions, tables, columns, import, sppages, spwidgets, uipages, appmenu, scrapi, acls, roles, properties)

**Node work commands**: Uses `_ticket.js` pattern for ticket-like entities (incidents, changes, requests, tasks, tickets) — a builder that takes a `tableName` and optionally custom flags.

### 10.3 Cross-Platform Considerations

- Both versions already handle Linux, macOS, and Windows
- Go uses build tags for platform-specific code; Node uses `process.platform`
- Credential paths are XDG-compliant in both versions
- Browser opening uses platform-appropriate commands

---

## 11. Recommendations

### Immediate (Phase 1)
1. Add `create`, `update`, `delete` to `jsn users`
2. Add `create`, `update`, `delete` to `jsn groups`  
3. Add `add`, `remove` to `jsn groupmembers`
4. Add `add`, `remove` to `jsn grouproles`
5. Add `create`, `update`, `delete` to `jsn dev flows`

### Short-term (Phase 2)
6. Add missing SDK methods: `GetCurrentUpdateSet`, `GetCurrentApplication`, `FetchAttachments`, `FetchCatalogVariables`
7. Add `SERVICENOW_OAUTH_CLIENT_ID` and `SERVICENOW_OAUTH_TOKEN` env var support to Node.js auth
8. Add `approval_prompt=force` to Node.js auth URL builder
9. Write per-command tests with mock HTTP transport

### Medium-term (Phase 3)
10. Add built-in jq-style filtering to Node.js output
11. Add scope + update set display to interactive sessions
12. Add richer record formatting (field grouping, related links)
13. Eventually deprecate Go version and remove from repo

---

## Appendix: Quick Reference

### Go-only files (will become dead code after migration)
```
internal/commands/dev/acls.go          -> _simple.js
internal/commands/dev/businessrules.go -> _simple.js
internal/commands/dev/clientscripts.go -> _simple.js
internal/commands/dev/columns.go       -> _simple.js
internal/commands/dev/imports.go       -> _simple.js
internal/commands/dev/properties.go    -> _simple.js
internal/commands/dev/roles.go         -> _simple.js
internal/commands/dev/scopes.go        -> scopes.js (custom)
internal/commands/dev/scriptincludes.go -> _simple.js
internal/commands/dev/tables.go        -> _simple.js
internal/commands/dev/uiactions.go     -> _simple.js
internal/commands/dev/uipages.go       -> _simple.js
internal/commands/dev/uipolicies.go    -> _simple.js
internal/commands/dev/updatesets.go    -> updatesets.js (custom)
internal/tui/*                         -> @inquirer/search
internal/output/*                      -> src/output.js + src/errors.js
internal/appctx/*                      -> src/app.js + src/context.js
internal/cli/root.go                   -> src/cli.js
cmd/jsn/main.go                        -> bin/jsn.js
```

### Node.js-only features (keep, port to Go if desired)
```
dev updatesets create        -> Go doesn't have this
dev scopes set               -> Go doesn't have this
```
