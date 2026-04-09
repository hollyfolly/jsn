# Flow Designer CLI

Building and managing ServiceNow Flow Designer flows from the command line.

## What Works Today

### Create Flows & Subflows

```bash
# Interactive — prompts for everything
jsn flows create

# Non-interactive
jsn flows create --name "My Flow" --type flow
jsn flows create --name "My Helper" --type subflow --run-as system
```

Subflows support typed inputs and outputs:

```bash
jsn flows create --name "Update Ticket" --type subflow \
  --input "ticket_id:string:Ticket ID:true" \
  --input "new_state:choice:New State:true" \
  --output "success:boolean:Success"
```

Supported variable types: `string`, `integer`, `boolean`, `reference`, `choice`, `date`, `datetime`.

### Add Triggers

Triggers use the same GraphQL API that Flow Designer's UI uses — they persist correctly and show up in the browser.

**Record Triggers:**

```bash
# Interactive (after flow creation, or standalone)
jsn flows add-trigger "My Flow" --type record_create --table incident
jsn flows add-trigger "My Flow" --type record_update --table incident --condition "priority=1"
jsn flows add-trigger "My Flow" --type record_create_or_update --table change_request
```

**Scheduled Triggers:**

```bash
jsn flows add-trigger "My Flow" --schedule daily --time "08:00:00"
jsn flows add-trigger "My Flow" --schedule weekly --day 3 --time "09:30:00"
jsn flows add-trigger "My Flow" --schedule monthly --day 15 --time "14:00:00"
jsn flows add-trigger "My Flow" --schedule repeat --repeat "2d 4h 30m"
jsn flows add-trigger "My Flow" --schedule once --date "2026-06-15 10:00:00"
```

**Application Triggers:**

```bash
jsn flows add-trigger "My Flow" --type service_catalog
```

### Inspect Flows

```bash
jsn flows                              # List all flows
jsn flows "My Flow"                    # Show flow structure
jsn flows "My Flow" --json             # Raw JSON
jsn flows <sys_id>                     # By sys_id
```

## How It Works Under the Hood

### API Strategy

ServiceNow's Table API doesn't work for flow components — business rules interfere and silently drop data. We use the same APIs that Flow Designer's browser UI uses:

1. **Processflow API** — `POST /api/now/processflow/flow` — creates the flow record + version atomically
2. **GraphQL API** — `POST /api/now/graphql` with `snFlowDesigner` mutations — adds triggers (and eventually actions, logic, subflows)

### Safe Edit Lock

Before any GraphQL mutation, we acquire a "safe edit" lock on the flow (a record in `sys_hub_flow_safe_edit`). This is the same locking mechanism the browser uses. The lock is cleaned up automatically after the mutation.

### Trigger Definition IDs

Each trigger type has a well-known `triggerDefinitionId` in `sys_hub_trigger_definition`:

| Type | Definition ID |
|---|---|
| record_create | `798916a0c31322002841b63b12d3ae7c` |
| record_update | `bb695e60c31322002841b63b12d3aea5` |
| record_create_or_update | `a45d9180c32222002841b63b12d3aea7` |
| daily | `89142dc0c32222002841b63b12d3ae8a` |
| weekly | `cf352104c32222002841b63b12d3ae1f` |
| monthly | `2ca52504c32222002841b63b12d3ae4a` |
| run_once | `0a76e504c32222002841b63b12d3aeac` |
| repeat | `f63f0d94c32222002841b63b12d3aeed` |
| service_catalog | `c43a1011c36813002841b63b12d3ae15` |

Each trigger type also has its own set of well-known parameter IDs for inputs and outputs. These are hardcoded in the SDK because they're platform constants — they don't change between instances.

## What's Next

### Known Issues

- **Weekly, monthly, and once triggers**: API returns success but triggers don't always appear in the flow version payload. Daily and repeat triggers work correctly. Needs debugging — likely a subtle difference in mutation structure.
- **Trigger detail display**: `jsn flows <name>` shows trigger type but doesn't yet display all inputs (repeat interval, day of week, day of month, run-at datetime). Only `table` and `time` are shown.

### Phase 3: Actions

Add actions to flows via the GraphQL API. Same pattern as triggers — capture what the browser sends, replicate it.

Common action types to support first:
- **Log** — simplest, good for testing the action insertion pipeline
- **Create Record** — insert a new record
- **Update Record** — modify an existing record
- **Look Up Record** — query for records
- **Delete Record** — remove a record

More complex actions for later:
- **Ask For Approval** — approval workflows
- **Wait For Condition** — async pause
- **Send Email / Notification**
- **Create Task**
- **Fire Event**

```bash
# Target CLI interface
jsn flows add-action "My Flow" --type create_record --table incident \
  --input "short_description=New from flow"

jsn flows add-action "My Flow" --type update_record \
  --input "record={{trigger.current}}" \
  --input "priority=1"

jsn flows add-action "My Flow" --type log --message "Flow executed"
```

### Phase 4: Logic Blocks

Add If/Else, Switch, and Loop constructs to flows.

```bash
jsn flows add-logic "My Flow" --type if \
  --condition "{{trigger.current.priority}}=1" \
  --name "High Priority"
```

### Phase 5: Subflow Calls

Call subflows from parent flows, mapping inputs and using outputs.

```bash
jsn flows add-subflow "My Flow" --subflow "My Helper" \
  --map "ticket_id={{trigger.current.sys_id}}"
```

### Phase 6: Templates

Pre-built flow patterns for common use cases:

```bash
jsn flows create --template approval --table change_request
jsn flows create --template notification --table incident --condition "priority=1"
```

## Data Model Reference

**Key Tables:**
- `sys_hub_flow` — flow definition
- `sys_hub_flow_version` — version records (contains the full payload JSON)
- `sys_hub_action_instance` / `sys_hub_action_instance_v2` — action placements
- `sys_hub_trigger_instance` / `sys_hub_trigger_instance_v2` — trigger definitions
- `sys_hub_flow_logic` / `sys_hub_flow_logic_instance_v2` — logic blocks (If/Else)
- `sys_hub_sub_flow_instance` — subflow calls
- `sys_hub_flow_input` / `sys_hub_flow_output` — subflow inputs/outputs

**Inspecting existing flows for reference:**

```bash
jsn flows "test"                            # 10 actions, 1 logic block — good reference
jsn flows "Add Role to User Or Group"       # subflow with 6 inputs, 2 outputs, 11 logic blocks
jsn flows "Software Procurement Flow"       # 30 actions, catalog triggers
jsn flows "Create Task"                     # minimal subflow, 2 actions
```

## File Layout

- `internal/sdk/flows.go` — all SDK functions: create flow/subflow, add triggers (GraphQL), inspect flows
- `internal/commands/flows.go` — CLI commands, interactive prompts, flag handling
