package sdk

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// Trigger definition IDs from sys_hub_trigger_definition table.
// These are standard ServiceNow sys_ids that map trigger types to their definitions.
var triggerDefinitionIDs = map[string]string{
	"record_create":           "798916a0c31322002841b63b12d3ae7c",
	"record_update":           "bb695e60c31322002841b63b12d3aea5",
	"record_create_or_update": "a45d9180c32222002841b63b12d3aea7",
	"daily":                   "89142dc0c32222002841b63b12d3ae8a",
	"weekly":                  "cf352104c32222002841b63b12d3ae1f",
	"monthly":                 "2ca52504c32222002841b63b12d3ae4a",
	"once":                    "0a76e504c32222002841b63b12d3aeac",
	"repeat":                  "f63f0d94c32222002841b63b12d3aeed",
	"service_catalog":         "c43a1011c36813002841b63b12d3ae15",
}

// Flow represents a ServiceNow Flow Designer flow (sys_hub_flow record).
type Flow struct {
	SysID       string `json:"sys_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Active      bool   `json:"active,string"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	SysScope    string `json:"sys_scope"`
	Version     string `json:"version"`
	RunAs       string `json:"run_as"`
	RunAsUser   string `json:"run_as_user"`
	CreatedOn   string `json:"sys_created_on"`
	CreatedBy   string `json:"sys_created_by"`
	UpdatedOn   string `json:"sys_updated_on"`
	UpdatedBy   string `json:"sys_updated_by"`
}

// ListFlowsOptions holds options for listing flows.
type ListFlowsOptions struct {
	Limit     int
	Offset    int
	Query     string
	OrderBy   string
	OrderDesc bool
}

// ListFlows retrieves flows from sys_hub_flow.
func (c *Client) ListFlows(ctx context.Context, opts *ListFlowsOptions) ([]Flow, error) {
	if opts == nil {
		opts = &ListFlowsOptions{}
	}

	query := url.Values{}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	query.Set("sysparm_limit", fmt.Sprintf("%d", limit))

	if opts.Offset > 0 {
		query.Set("sysparm_offset", fmt.Sprintf("%d", opts.Offset))
	}

	query.Set("sysparm_fields", "sys_id,name,type,active,description,scope,sys_scope,version,run_as,run_as_user,sys_created_on,sys_created_by,sys_updated_on,sys_updated_by")

	orderBy := opts.OrderBy
	if orderBy == "" {
		orderBy = "name"
	}

	var sysparmQuery string
	if opts.OrderDesc {
		sysparmQuery = "ORDERBYDESC" + orderBy
	} else {
		sysparmQuery = "ORDERBY" + orderBy
	}

	if opts.Query != "" {
		sysparmQuery = sysparmQuery + "^" + opts.Query
	}

	query.Set("sysparm_query", sysparmQuery)

	resp, err := c.Get(ctx, "sys_hub_flow", query)
	if err != nil {
		return nil, err
	}

	flows := make([]Flow, len(resp.Result))
	for i, record := range resp.Result {
		flows[i] = flowFromRecord(record)
	}

	return flows, nil
}

// GetFlow retrieves a single flow by name or sys_id.
func (c *Client) GetFlow(ctx context.Context, identifier string) (*Flow, error) {
	query := url.Values{}
	query.Set("sysparm_limit", "1")
	query.Set("sysparm_fields", "sys_id,name,type,active,description,scope,sys_scope,version,run_as,run_as_user,sys_created_on,sys_created_by,sys_updated_on,sys_updated_by")

	// Check if identifier looks like a sys_id (32 character hex string)
	if len(identifier) == 32 {
		query.Set("sysparm_query", fmt.Sprintf("sys_id=%s", identifier))
	} else {
		query.Set("sysparm_query", fmt.Sprintf("name=%s", identifier))
	}

	resp, err := c.Get(ctx, "sys_hub_flow", query)
	if err != nil {
		return nil, err
	}

	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("flow not found: %s", identifier)
	}

	flow := flowFromRecord(resp.Result[0])
	return &flow, nil
}

// flowFromRecord converts a record map to a Flow struct.
func flowFromRecord(record map[string]interface{}) Flow {
	return Flow{
		SysID:       getString(record, "sys_id"),
		Name:        getString(record, "name"),
		Type:        getString(record, "type"),
		Active:      getBool(record, "active"),
		Description: getString(record, "description"),
		Scope:       getString(record, "scope"),
		SysScope:    getString(record, "sys_scope"),
		Version:     getString(record, "version"),
		RunAs:       getString(record, "run_as"),
		RunAsUser:   getString(record, "run_as_user"),
		CreatedOn:   getString(record, "sys_created_on"),
		CreatedBy:   getString(record, "sys_created_by"),
		UpdatedOn:   getString(record, "sys_updated_on"),
		UpdatedBy:   getString(record, "sys_updated_by"),
	}
}

// FlowExecution represents a flow execution record from sys_hub_trigger_instance_v2.
type FlowExecution struct {
	SysID        string `json:"sys_id"`
	FlowID       string `json:"flow_id"`
	FlowName     string `json:"flow_name"`
	Status       string `json:"status"`
	Started      string `json:"started"`
	Ended        string `json:"ended"`
	Duration     string `json:"duration"`
	SysUpdatedOn string `json:"sys_updated_on"`
}

// ListFlowExecutionsOptions holds options for listing flow executions.
type ListFlowExecutionsOptions struct {
	FlowID    string
	Limit     int
	Offset    int
	OrderBy   string
	OrderDesc bool
}

// ListFlowExecutions retrieves flow execution history from sys_hub_trigger_instance_v2.
func (c *Client) ListFlowExecutions(ctx context.Context, opts *ListFlowExecutionsOptions) ([]FlowExecution, error) {
	if opts == nil {
		opts = &ListFlowExecutionsOptions{}
	}

	query := url.Values{}

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	query.Set("sysparm_limit", fmt.Sprintf("%d", limit))

	if opts.Offset > 0 {
		query.Set("sysparm_offset", fmt.Sprintf("%d", opts.Offset))
	}

	query.Set("sysparm_fields", "sys_id,flow,flow.name,status,started,ended,duration,sys_updated_on")

	orderBy := opts.OrderBy
	if orderBy == "" {
		orderBy = "sys_updated_on"
	}

	var sysparmQuery string
	if opts.OrderDesc {
		sysparmQuery = "ORDERBYDESC" + orderBy
	} else {
		sysparmQuery = "ORDERBY" + orderBy
	}

	if opts.FlowID != "" {
		sysparmQuery = sysparmQuery + "^flow=" + opts.FlowID
	}

	query.Set("sysparm_query", sysparmQuery)

	resp, err := c.Get(ctx, "sys_hub_trigger_instance_v2", query)
	if err != nil {
		return nil, err
	}

	executions := make([]FlowExecution, len(resp.Result))
	for i, record := range resp.Result {
		executions[i] = flowExecutionFromRecord(record)
	}

	return executions, nil
}

// flowExecutionFromRecord converts a record map to a FlowExecution struct.
func flowExecutionFromRecord(record map[string]interface{}) FlowExecution {
	// Handle flow.name which might be a display value object
	flowName := ""
	if flow, ok := record["flow"].(map[string]interface{}); ok {
		flowName = getString(flow, "display_value")
		if flowName == "" {
			flowName = getString(flow, "value")
		}
	}

	return FlowExecution{
		SysID:        getString(record, "sys_id"),
		FlowID:       getString(record, "flow"),
		FlowName:     flowName,
		Status:       getString(record, "status"),
		Started:      getString(record, "started"),
		Ended:        getString(record, "ended"),
		Duration:     getString(record, "duration"),
		SysUpdatedOn: getString(record, "sys_updated_on"),
	}
}

// FlowAction represents a flow action instance.
type FlowAction struct {
	SysID  string `json:"sys_id"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Order  int    `json:"order"`
	Active bool   `json:"active"`
	FlowID string `json:"flow_id"`
}

// GetFlowActions retrieves actions for a flow from sys_hub_action_instance and sys_hub_action_instance_v2.
func (c *Client) GetFlowActions(ctx context.Context, flowID string) ([]FlowAction, error) {
	var allActions []FlowAction

	// Check V1 action instances
	query := url.Values{}
	query.Set("sysparm_limit", "100")
	query.Set("sysparm_fields", "sys_id,name,action_type,order,active,flow")
	query.Set("sysparm_query", fmt.Sprintf("flow=%s^ORDERBYorder", flowID))

	resp, err := c.Get(ctx, "sys_hub_action_instance", query)
	if err == nil {
		for _, record := range resp.Result {
			allActions = append(allActions, flowActionFromRecord(record))
		}
	}

	// Check V2 action instances
	queryV2 := url.Values{}
	queryV2.Set("sysparm_limit", "100")
	queryV2.Set("sysparm_fields", "sys_id,action_type,order,flow,values")
	queryV2.Set("sysparm_query", fmt.Sprintf("flow=%s^ORDERBYorder", flowID))

	respV2, err := c.Get(ctx, "sys_hub_action_instance_v2", queryV2)
	if err == nil {
		for _, record := range respV2.Result {
			allActions = append(allActions, flowActionFromRecordV2(record))
		}
	}

	return allActions, nil
}

// flowActionFromRecord converts a record map to a FlowAction struct.
func flowActionFromRecord(record map[string]interface{}) FlowAction {
	// Handle action_type which may be a reference field
	actionType := getDisplayValue(record, "action_type")
	if actionType == "" {
		// Try to extract from reference field value or link
		if at, ok := record["action_type"].(map[string]interface{}); ok {
			actionType = getString(at, "value")
			if actionType == "" {
				// Extract from link URL (e.g., .../sys_hub_action_type_base/core.log)
				link := getString(at, "link")
				if link != "" {
					// Parse the last part of the URL path
					for i := len(link) - 1; i >= 0; i-- {
						if link[i] == '/' {
							actionType = link[i+1:]
							break
						}
					}
				}
			}
		}
		if actionType == "" {
			actionType = getString(record, "action_type")
		}
	}

	return FlowAction{
		SysID:  getString(record, "sys_id"),
		Name:   getString(record, "name"),
		Action: actionType,
		Order:  getInt(record, "order"),
		Active: getBool(record, "active"),
		FlowID: getString(record, "flow"),
	}
}

// flowActionFromRecordV2 converts a V2 action record map to a FlowAction struct.
func flowActionFromRecordV2(record map[string]interface{}) FlowAction {
	// Handle action_type which may be a reference field
	actionType := getDisplayValue(record, "action_type")
	if actionType == "" {
		if at, ok := record["action_type"].(map[string]interface{}); ok {
			actionType = getString(at, "value")
		}
		if actionType == "" {
			actionType = getString(record, "action_type")
		}
	}

	return FlowAction{
		SysID:  getString(record, "sys_id"),
		Name:   "", // V2 doesn't have a name field directly
		Action: actionType,
		Order:  getInt(record, "order"),
		Active: true, // V2 actions are active by default
		FlowID: getString(record, "flow"),
	}
}

// FlowVariable represents a flow variable definition.
type FlowVariable struct {
	SysID string `json:"sys_id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// GetFlowVariables retrieves variables for a flow.
func (c *Client) GetFlowVariables(ctx context.Context, flowID string) ([]FlowVariable, error) {
	query := url.Values{}
	query.Set("sysparm_limit", "100")
	query.Set("sysparm_fields", "sys_id,name,variable_type,label,default_value")
	query.Set("sysparm_query", fmt.Sprintf("flow=%s", flowID))

	resp, err := c.Get(ctx, "sys_hub_flow_variable", query)
	if err != nil {
		return nil, err
	}

	variables := make([]FlowVariable, len(resp.Result))
	for i, record := range resp.Result {
		variables[i] = flowVariableFromRecord(record)
	}

	return variables, nil
}

// flowVariableFromRecord converts a record map to a FlowVariable struct.
func flowVariableFromRecord(record map[string]interface{}) FlowVariable {
	return FlowVariable{
		SysID: getString(record, "sys_id"),
		Name:  getString(record, "name"),
		Type:  getString(record, "variable_type"),
		Label: getString(record, "label"),
		Value: getString(record, "default_value"),
	}
}

// UpdateFlowStatus activates or deactivates a flow.
func (c *Client) UpdateFlowStatus(ctx context.Context, flowID string, active bool) error {
	updates := map[string]interface{}{
		"active": active,
	}
	_, err := c.Patch(ctx, "sys_hub_flow", flowID, updates)
	return err
}

// ExecuteFlowInput holds parameters for executing a flow.
type ExecuteFlowInput struct {
	Inputs map[string]interface{} // Flow input variables
}

// ExecuteFlow manually executes/triggers a flow.
// This creates a flow execution record and starts the flow.
func (c *Client) ExecuteFlow(ctx context.Context, flowID string, input ExecuteFlowInput) (*FlowExecution, error) {
	// Create a trigger instance to execute the flow
	data := map[string]interface{}{
		"flow":   flowID,
		"status": "waiting",
	}

	// Add any input variables if provided
	if len(input.Inputs) > 0 {
		inputJSON, _ := json.Marshal(input.Inputs)
		data["inputs"] = string(inputJSON)
	}

	resp, err := c.Post(ctx, "sys_hub_trigger_instance", data)
	if err != nil {
		return nil, fmt.Errorf("failed to execute flow: %w", err)
	}

	if resp.Result == nil {
		return nil, fmt.Errorf("no response from execute flow")
	}

	// Get the execution details
	exec := FlowExecution{
		SysID:        getString(resp.Result, "sys_id"),
		FlowID:       flowID,
		Status:       getString(resp.Result, "status"),
		Started:      getString(resp.Result, "sys_created_on"),
		SysUpdatedOn: getString(resp.Result, "sys_updated_on"),
	}

	return &exec, nil
}

// FlowInspection holds comprehensive data about a flow for debugging.
type FlowInspection struct {
	Flow               *Flow
	Version            map[string]interface{}
	Components         []map[string]interface{}
	TriggerInstances   []map[string]interface{}
	ActionInstances    []map[string]interface{}
	ActionInstancesV2  []map[string]interface{}
	FlowLogicInstances []map[string]interface{}
	SubFlowInstances   []map[string]interface{}
	FlowInputs         []map[string]interface{}
	FlowOutputs        []map[string]interface{}
}

// InspectFlow retrieves comprehensive information about a flow for debugging.
func (c *Client) InspectFlow(ctx context.Context, flowID string) (*FlowInspection, error) {
	inspection := &FlowInspection{}

	// Get the flow
	flow, err := c.GetFlow(ctx, flowID)
	if err != nil {
		return nil, err
	}
	inspection.Flow = flow

	// Get version record
	versionQuery := url.Values{}
	versionQuery.Set("sysparm_query", fmt.Sprintf("flow=%s^ORDERBYDESCsys_updated_on", flowID))
	versionQuery.Set("sysparm_limit", "1")
	if resp, err := c.Get(ctx, "sys_hub_flow_version", versionQuery); err == nil && len(resp.Result) > 0 {
		inspection.Version = resp.Result[0]

		// Parse payload to extract trigger configuration (time, name, etc.)
		if payload, ok := resp.Result[0]["payload"].(string); ok && payload != "" {
			var payloadData map[string]interface{}
			if err := json.Unmarshal([]byte(payload), &payloadData); err == nil {
				// Extract trigger info from triggerInstances
				if triggerInstances, ok := payloadData["triggerInstances"].([]interface{}); ok && len(triggerInstances) > 0 {
					if firstTrigger, ok := triggerInstances[0].(map[string]interface{}); ok {
						// Extract trigger name
						if triggerName, ok := firstTrigger["name"].(string); ok && triggerName != "" {
							resp.Result[0]["trigger_name"] = triggerName
						}
						// Extract trigger type
						if triggerType, ok := firstTrigger["type"].(string); ok && triggerType != "" {
							resp.Result[0]["trigger_type"] = triggerType
						}
						// Extract trigger table and time from inputs
						if inputs, ok := firstTrigger["inputs"].([]interface{}); ok {
							for _, input := range inputs {
								if inputMap, ok := input.(map[string]interface{}); ok {
									if name, ok := inputMap["name"].(string); ok {
										if name == "time" {
											if value, ok := inputMap["value"].(string); ok && value != "" {
												resp.Result[0]["trigger_time"] = value
											}
										}
										if name == "table" {
											if value, ok := inputMap["value"].(string); ok && value != "" {
												resp.Result[0]["trigger_table"] = value
											}
										}
									}
								}
							}
						}
					}
				}
				// Extract flow logic instances (If/Then/Else conditions)
				if flowLogic, ok := payloadData["flowLogicInstances"].([]interface{}); ok {
					inspection.FlowLogicInstances = make([]map[string]interface{}, 0, len(flowLogic))
					for _, logic := range flowLogic {
						if logicMap, ok := logic.(map[string]interface{}); ok {
							inspection.FlowLogicInstances = append(inspection.FlowLogicInstances, logicMap)
						}
					}
				}
				// Extract action instances from payload (they have parent references to logic)
				if actionInstances, ok := payloadData["actionInstances"].([]interface{}); ok {
					for _, action := range actionInstances {
						if actionMap, ok := action.(map[string]interface{}); ok {
							// Store action instances from payload for full structure
							// These have parent references showing the flow structure
							inspection.ActionInstances = append(inspection.ActionInstances, actionMap)
						}
					}
				}

				// Extract subflow instances from payload (calls to other flows)
				if subFlowInstances, ok := payloadData["subFlowInstances"].([]interface{}); ok {
					for _, subFlow := range subFlowInstances {
						if subFlowMap, ok := subFlow.(map[string]interface{}); ok {
							inspection.SubFlowInstances = append(inspection.SubFlowInstances, subFlowMap)
						}
					}
				}

				// Extract flow inputs from payload (for subflows)
				if inputs, ok := payloadData["inputs"].([]interface{}); ok {
					for _, input := range inputs {
						if inputMap, ok := input.(map[string]interface{}); ok {
							inspection.FlowInputs = append(inspection.FlowInputs, inputMap)
						}
					}
				}

				// Extract flow outputs from payload (for subflows)
				if outputs, ok := payloadData["outputs"].([]interface{}); ok {
					for _, output := range outputs {
						if outputMap, ok := output.(map[string]interface{}); ok {
							inspection.FlowOutputs = append(inspection.FlowOutputs, outputMap)
						}
					}
				}
			}
		}
	}

	// Get flow components
	compQuery := url.Values{}
	compQuery.Set("sysparm_query", fmt.Sprintf("flow=%s", flowID))
	compQuery.Set("sysparm_fields", "sys_id,sys_class_name,order,display_text,ui_id,parent_ui_id,attributes")
	if resp, err := c.Get(ctx, "sys_hub_flow_component", compQuery); err == nil {
		inspection.Components = resp.Result
	}

	// Get trigger instances
	triggerQuery := url.Values{}
	triggerQuery.Set("sysparm_query", fmt.Sprintf("flow=%s", flowID))
	triggerQuery.Set("sysparm_fields", "sys_id,name,trigger_definition,trigger_type,display_text,active")
	if resp, err := c.Get(ctx, "sys_hub_trigger_instance", triggerQuery); err == nil {
		inspection.TriggerInstances = resp.Result
	}

	// NOTE: sys_flow_timer_trigger, sys_flow_record_trigger, and sys_hub_trigger_definition
	// do NOT have a 'flow' field, so querying them with flow={id} returns all records.
	// Trigger info comes from the version payload's triggerInstances or sys_hub_trigger_instance.

	// Get action instances (V1) — skip if version payload already provided them
	if len(inspection.ActionInstances) == 0 {
		actionQuery := url.Values{}
		actionQuery.Set("sysparm_query", fmt.Sprintf("flow=%s", flowID))
		actionQuery.Set("sysparm_fields", "sys_id,action_type,order,active,comment,action_inputs,display_text,name")
		if resp, err := c.Get(ctx, "sys_hub_action_instance", actionQuery); err == nil {
			inspection.ActionInstances = resp.Result
			c.resolveActionTypeNames(ctx, inspection.ActionInstances)
		}
	}

	// Get action instances (V2)
	actionV2Query := url.Values{}
	actionV2Query.Set("sysparm_query", fmt.Sprintf("flow=%s", flowID))
	actionV2Query.Set("sysparm_fields", "sys_id,action_type,order,values,display_text")
	actionV2Query.Set("sysparm_display_value", "all")
	if resp, err := c.Get(ctx, "sys_hub_action_instance_v2", actionV2Query); err == nil {
		inspection.ActionInstancesV2 = resp.Result
		c.resolveActionTypeNames(ctx, inspection.ActionInstancesV2)
		// Decompress values field for each action instance
		for _, action := range inspection.ActionInstancesV2 {
			// Try to get values as a display_value/value map first
			var valueStr string
			if valuesField, ok := action["values"].(map[string]interface{}); ok {
				valueStr = getDisplayOrValue(valuesField, "value")
			} else if strValue, ok := action["values"].(string); ok {
				// Direct string value
				valueStr = strValue
			}
			if valueStr != "" {
				if decompressed, err := decompressFlowValues(valueStr); err == nil && decompressed != nil {
					action["values_decompressed"] = decompressed
				}
			}
		}
	}

	// Get flow logic instances — skip if version payload already provided them
	if len(inspection.FlowLogicInstances) == 0 {
		// Query both V1 and V2 flow logic tables
		for _, table := range []string{"sys_hub_flow_logic", "sys_hub_flow_logic_instance_v2"} {
			logicQuery := url.Values{}
			logicQuery.Set("sysparm_query", fmt.Sprintf("flow=%s^ORDERBYorder", flowID))
			logicQuery.Set("sysparm_fields", "sys_id,order,logic_definition,display_text,parent_ui_id,comment,values")
			logicQuery.Set("sysparm_display_value", "all")
			logicQuery.Set("sysparm_limit", "50")
			if resp, err := c.Get(ctx, table, logicQuery); err == nil {
				for _, record := range resp.Result {
					// Normalize display_value fields for consistent downstream use
					logicMap := map[string]interface{}{
						"sys_id":       getDisplayOrValue(record, "sys_id"),
						"order":        getDisplayOrValue(record, "order"),
						"name":         getDisplayOrValue(record, "logic_definition"),
						"comment":      getDisplayOrValue(record, "comment"),
						"display_text": getDisplayOrValue(record, "display_text"),
						"parent_ui_id": getDisplayOrValue(record, "parent_ui_id"),
						"source_table": table,
					}
					// Decompress values field for V2 logic instances
					if table == "sys_hub_flow_logic_instance_v2" {
						if valuesField, ok := record["values"].(map[string]interface{}); ok {
							if valueStr := getDisplayOrValue(valuesField, "value"); valueStr != "" {
								if decompressed, err := decompressFlowValues(valueStr); err == nil && decompressed != nil {
									logicMap["values_decompressed"] = decompressed
								}
							}
						}
					}
					inspection.FlowLogicInstances = append(inspection.FlowLogicInstances, logicMap)
				}
			}
		}
	}

	// Get subflow instances — skip if version payload already provided them
	if len(inspection.SubFlowInstances) == 0 {
		sfQuery := url.Values{}
		sfQuery.Set("sysparm_query", fmt.Sprintf("flow=%s^ORDERBYorder", flowID))
		sfQuery.Set("sysparm_fields", "sys_id,order,sub_flow,display_text,parent_ui_id,comment")
		sfQuery.Set("sysparm_display_value", "all")
		sfQuery.Set("sysparm_limit", "50")
		if resp, err := c.Get(ctx, "sys_hub_sub_flow_instance", sfQuery); err == nil {
			for _, record := range resp.Result {
				sfMap := map[string]interface{}{
					"sys_id":       getDisplayOrValue(record, "sys_id"),
					"order":        getDisplayOrValue(record, "order"),
					"name":         getDisplayOrValue(record, "sub_flow"),
					"comment":      getDisplayOrValue(record, "comment"),
					"display_text": getDisplayOrValue(record, "display_text"),
					"parent_ui_id": getDisplayOrValue(record, "parent_ui_id"),
				}
				inspection.SubFlowInstances = append(inspection.SubFlowInstances, sfMap)
			}
		}
	}

	return inspection, nil
}

// resolveActionTypeNames looks up display names for action_type references
// and sets the display_value on each action's action_type map.
func (c *Client) resolveActionTypeNames(ctx context.Context, actions []map[string]interface{}) {
	cache := make(map[string]string)
	for _, action := range actions {
		if at, ok := action["action_type"].(map[string]interface{}); ok {
			actionID := getString(at, "value")
			if actionID == "" {
				continue
			}
			if name, found := cache[actionID]; found {
				at["display_value"] = name
				continue
			}
			typeQuery := url.Values{}
			typeQuery.Set("sysparm_query", fmt.Sprintf("sys_id=%s", actionID))
			typeQuery.Set("sysparm_fields", "sys_id,name")
			typeQuery.Set("sysparm_limit", "1")
			if typeResp, err := c.Get(ctx, "sys_hub_action_type_base", typeQuery); err == nil && len(typeResp.Result) > 0 {
				if name := getString(typeResp.Result[0], "name"); name != "" {
					at["display_value"] = name
					cache[actionID] = name
				}
			}
		}
	}
}

// getDisplayOrValue extracts the value from a field that may be a display_value/value
// pair (from sysparm_display_value=all) or a plain string.
func getDisplayOrValue(record map[string]interface{}, key string) string {
	val := record[key]
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	if m, ok := val.(map[string]interface{}); ok {
		if dv, ok := m["display_value"].(string); ok && dv != "" {
			return dv
		}
		if v, ok := m["value"].(string); ok {
			return v
		}
	}
	return ""
}

// decompressFlowValues decompresses gzipped, base64-encoded flow data.
// This handles the values field from sys_hub_action_instance_v2 and
// sys_hub_flow_logic_instance_v2 tables.
// Note: ServiceNow sometimes returns corrupted gzip data that cannot be
// fully decompressed. In these cases, we return nil and the caller should
// fall back to the flow version payload which contains the same data in plain JSON.
func decompressFlowValues(value string) ([]map[string]interface{}, error) {
	if value == "" {
		return nil, nil
	}

	// Check if it looks like base64 (starts with common gzip magic bytes in base64)
	// H4sI = gzip magic bytes in base64
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		// Not base64, try parsing as plain JSON
		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(value), &result); jsonErr == nil {
			return []map[string]interface{}{result}, nil
		}
		return nil, fmt.Errorf("failed to decode value: %w", err)
	}

	// Try gzip decompress
	reader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		// Not gzipped, try parsing decoded bytes as JSON
		var result []map[string]interface{}
		if jsonErr := json.Unmarshal(decoded, &result); jsonErr == nil {
			return result, nil
		}
		// Try as single object
		var objResult map[string]interface{}
		if jsonErr := json.Unmarshal(decoded, &objResult); jsonErr == nil {
			return []map[string]interface{}{objResult}, nil
		}
		return nil, fmt.Errorf("failed to decompress value: %w", err)
	}
	defer reader.Close()

	// Read decompressed data - ignore errors as ServiceNow sometimes sends
	// corrupted gzip data with invalid checksums
	decompressed, _ := io.ReadAll(reader)
	if len(decompressed) == 0 {
		return nil, fmt.Errorf("no data decompressed")
	}

	// Try to parse as JSON array
	var result []map[string]interface{}
	if err := json.Unmarshal(decompressed, &result); err == nil {
		return result, nil
	}

	// Try as single object
	var objResult map[string]interface{}
	if err := json.Unmarshal(decompressed, &objResult); err == nil {
		return []map[string]interface{}{objResult}, nil
	}

	return nil, fmt.Errorf("failed to parse decompressed JSON: %w", err)
}

// CreateFlowOptions holds options for creating a new flow.
type CreateFlowOptions struct {
	Name        string // Required: Flow name
	Type        string // "flow" or "subflow" (default: "flow")
	Description string // Optional: Flow description
	Active      bool   // Default: false
	RunAs       string // "user" or "system" (default: "user")
	Scope       string // Optional: defaults to user's current scope
}

// buildInitialFlowPayload creates the initial empty-shell payload for a new
// flow or subflow version record. This matches the format Flow Designer expects.
func buildInitialFlowPayload(flowSysID, name, description, runAs, internalName, now string, active bool) map[string]interface{} {
	return map[string]interface{}{
		"id": flowSysID, "masterSnapshotId": "", "name": name, "updatedBy": "admin",
		"triggerInstances": []interface{}{}, "actionInstances": []interface{}{},
		"flowLogicInstances": []interface{}{}, "subFlowInstances": []interface{}{},
		"created": now, "updated": now, "deleted": false, "description": description,
		"scope": "global", "scopeDisplayName": "Global", "scopeName": "global", "scopeLogo": "",
		"scopeProtectionPolicyReadOnly": true, "isSnapshot": false, "status": "draft",
		"fLatestSnapshot": "", "active": active,
		"security": map[string]interface{}{
			"fCanRead": true, "fCanWrite": true, "fValidDelegatedDeveloperData": true,
			"fReason": "", "fDDInFlowDesigner": false, "fDDAccessScopes": "",
			"fCanCancelExecution": "not_applicable",
		},
		"protection": "", "canWriteProtection": false, "fIsMasterSnapshot": false,
		"inputs": []interface{}{}, "outputs": []interface{}{},
		"type": "flow", "annotation": "", "natlang": "", "category": "",
		"remoteTriggerSysId": "", "access": "public", "serviceCatalogCallable": false,
		"clientCallable": false, "stages": map[string]interface{}{}, "copiedFrom": "",
		"internalName": internalName, "flowCatalogVariableModelId": "",
		"flowCatalogVariables": []interface{}{}, "runAs": runAs,
		"domainName": "global", "domainId": "global", "compilerBuild": "",
		"runWithRoles":           map[string]interface{}{"value": "", "displayValue": ""},
		"allowHighSecurityRoles": false, "userHasRolesAssignedToFlow": false,
		"flowVariables": []interface{}{}, "isFlowEntitled": true,
		"nonCriticalErrors":                     []interface{}{},
		"pharmacy":                              map[string]interface{}{"pharmacyCompound": map[string]interface{}{}},
		"startingIndexOfErrorHandlingInstances": -1,
		"connectionConfigurations":              []interface{}{}, "engineVersion": 0,
		"flowPriority": "MEDIUM", "fUserCanRead": true, "isJsonSnapshot": false,
		"attributes": map[string]interface{}{}, "authoredOnReleaseVersion": 29000,
		"isSavedAsJson": false, "version": "1", "generationSource": "",
		"displayNameAfterPreview": "", "substatus": "",
	}
}

// CreateFlow creates a new flow in ServiceNow.
// This creates both the sys_hub_flow record and an initial version record.
func (c *Client) CreateFlow(ctx context.Context, opts CreateFlowOptions) (*Flow, error) {
	// Validate required fields
	if opts.Name == "" {
		return nil, fmt.Errorf("flow name is required")
	}

	// Set defaults
	flowType := opts.Type
	if flowType == "" {
		flowType = "flow"
	}
	runAs := opts.RunAs
	if runAs == "" {
		runAs = "user"
	}

	// Create the flow record
	flowData := map[string]interface{}{
		"name":                        opts.Name,
		"type":                        flowType,
		"description":                 opts.Description,
		"active":                      opts.Active,
		"run_as":                      runAs,
		"flow_priority":               "MEDIUM",
		"authored_on_release_version": "29000",
	}

	if opts.Scope != "" {
		flowData["scope"] = opts.Scope
	}

	resp, err := c.Post(ctx, "sys_hub_flow", flowData)
	if err != nil {
		return nil, fmt.Errorf("failed to create flow: %w", err)
	}

	if resp.Result == nil {
		return nil, fmt.Errorf("no response from create flow")
	}

	flow := flowFromRecord(resp.Result)

	// Create initial version record with type="Autosave" — this matches the format
	// that Flow Designer uses. If we leave type empty, FD ignores our version and
	// creates its own "update" version that overwrites our trigger data.
	internalName := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(opts.Name), " ", ""), "-", "")
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	initialPayload := buildInitialFlowPayload(flow.SysID, opts.Name, opts.Description, runAs, internalName, now, opts.Active)
	payloadJSON, _ := json.Marshal(initialPayload)
	versionData := map[string]interface{}{
		"flow":    flow.SysID,
		"type":    "Autosave",
		"payload": string(payloadJSON),
	}

	_, err = c.Post(ctx, "sys_hub_flow_version", versionData)
	if err != nil {
		_ = c.Delete(ctx, "sys_hub_flow", flow.SysID)
		return nil, fmt.Errorf("failed to create flow version: %w", err)
	}

	return &flow, nil
}

// FlowVariableDef defines a flow input or output variable.
type FlowVariableDef struct {
	Name         string // Variable name (internal)
	Label        string // Display label
	Type         string // Variable type: string, integer, boolean, reference, etc.
	Mandatory    bool   // Whether the variable is required
	Reference    string // For reference types: the table name
	DefaultValue string // Default value
	Description  string // Variable description
}

// CreateSubflowOptions holds options for creating a new subflow.
type CreateSubflowOptions struct {
	Name        string            // Required: Subflow name
	Description string            // Optional: Subflow description
	Active      bool              // Default: false
	RunAs       string            // "user" or "system" (default: "user")
	Scope       string            // Optional: defaults to user's current scope
	Inputs      []FlowVariableDef // Input variables
	Outputs     []FlowVariableDef // Output variables
}

// CreateSubflow creates a new subflow with optional inputs and outputs.
func (c *Client) CreateSubflow(ctx context.Context, opts CreateSubflowOptions) (*Flow, error) {
	// Validate required fields
	if opts.Name == "" {
		return nil, fmt.Errorf("subflow name is required")
	}

	// Set defaults
	runAs := opts.RunAs
	if runAs == "" {
		runAs = "user"
	}

	// Create the flow record as a subflow
	flowData := map[string]interface{}{
		"name":        opts.Name,
		"type":        "subflow",
		"description": opts.Description,
		"active":      opts.Active,
		"run_as":      runAs,
	}

	if opts.Scope != "" {
		flowData["scope"] = opts.Scope
	}

	resp, err := c.Post(ctx, "sys_hub_flow", flowData)
	if err != nil {
		return nil, fmt.Errorf("failed to create subflow: %w", err)
	}

	if resp.Result == nil {
		return nil, fmt.Errorf("no response from create subflow")
	}

	flow := flowFromRecord(resp.Result)

	// Create initial version record with type="Autosave" and proper payload
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	internalName := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(opts.Name), " ", ""), "-", "")
	initialPayload := buildInitialFlowPayload(flow.SysID, opts.Name, opts.Description, runAs, internalName, now, opts.Active)
	payloadJSON, _ := json.Marshal(initialPayload)
	versionData := map[string]interface{}{
		"flow":    flow.SysID,
		"type":    "Autosave",
		"payload": string(payloadJSON),
	}

	_, err = c.Post(ctx, "sys_hub_flow_version", versionData)
	if err != nil {
		_ = c.Delete(ctx, "sys_hub_flow", flow.SysID)
		return nil, fmt.Errorf("failed to create subflow version: %w", err)
	}

	// Create input variables
	for _, input := range opts.Inputs {
		err := c.createFlowVariable(ctx, flow.SysID, input, "input")
		if err != nil {
			return nil, fmt.Errorf("failed to create input variable '%s': %w", input.Name, err)
		}
	}

	// Create output variables
	for _, output := range opts.Outputs {
		err := c.createFlowVariable(ctx, flow.SysID, output, "output")
		if err != nil {
			return nil, fmt.Errorf("failed to create output variable '%s': %w", output.Name, err)
		}
	}

	return &flow, nil
}

// createFlowVariable creates a single flow input or output variable.
func (c *Client) createFlowVariable(ctx context.Context, flowID string, def FlowVariableDef, direction string) error {
	variableData := map[string]interface{}{
		"flow":       flowID,
		"name":       def.Name,
		"label":      def.Label,
		"direction":  direction,
		"type":       def.Type,
		"mandatory":  def.Mandatory,
		"default":    def.DefaultValue,
		"attributes": def.Description,
	}

	if def.Reference != "" {
		variableData["reference"] = def.Reference
	}

	_, err := c.Post(ctx, "sys_hub_flow_input", variableData)
	if err != nil {
		// Try the alternative table name if the first fails
		_, err = c.Post(ctx, "sys_hub_flow_output", variableData)
	}
	return err
}

// CreateRecordTriggerOptions holds options for creating a record-based trigger.
type CreateRecordTriggerOptions struct {
	FlowID    string // Flow sys_id
	Table     string // Table name (e.g., "incident")
	Operation string // "create", "update", or "create_or_update"
	Condition string // Optional encoded query condition
}

// CreateRecordTrigger creates a record-based trigger for a flow.
// This creates a sys_hub_trigger_instance_v2 record with gzip+base64 encoded trigger_inputs,
// which is the correct way to persist triggers so they appear in Flow Designer UI.
func (c *Client) CreateRecordTrigger(ctx context.Context, opts CreateRecordTriggerOptions) error {
	if opts.FlowID == "" {
		return fmt.Errorf("flow ID is required")
	}
	if opts.Table == "" {
		return fmt.Errorf("table name is required")
	}

	// Map operation to trigger type
	triggerType := opts.Operation
	if triggerType == "" {
		triggerType = "record_create"
	} else if triggerType == "create" {
		triggerType = "record_create"
	} else if triggerType == "update" {
		triggerType = "record_update"
	} else if triggerType == "create_or_update" {
		triggerType = "record_create_or_update"
	}

	// Look up trigger definition ID
	triggerDefID, ok := triggerDefinitionIDs[triggerType]
	if !ok {
		return fmt.Errorf("unsupported trigger type: %s", triggerType)
	}

	// Resolve flow sys_id (may be passed as name)
	flowSysID, err := c.resolveFlowID(ctx, opts.FlowID)
	if err != nil {
		return fmt.Errorf("failed to resolve flow: %w", err)
	}

	// Use the Flow Designer GraphQL API — the same API the UI uses.
	// This handles version records, trigger_instance_v2, payload, etc. atomically.

	// Step 1: Get current user sys_id for the safe edit lock
	userSysID, err := c.getCurrentUserSysID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	// Step 2: Create safe edit record (Flow Designer's concurrency lock)
	safeEditResp, err := c.Post(ctx, "sys_hub_flow_safe_edit", map[string]interface{}{
		"flow": flowSysID,
		"user": userSysID,
	})
	if err != nil {
		return fmt.Errorf("failed to acquire safe edit lock: %w", err)
	}
	safeEditID := getString(safeEditResp.Result, "sys_id")

	// Step 3: Clean up safe edit record when done (regardless of success/failure)
	defer func() {
		if safeEditID != "" {
			_ = c.Delete(ctx, "sys_hub_flow_safe_edit", safeEditID)
		}
	}()

	// Step 4: Build and send GraphQL mutation to insert the trigger
	triggerName := getTriggerName(triggerType)
	condition := opts.Condition

	mutation := buildTriggerInsertMutation(flowSysID, triggerName, triggerDefID, triggerType, opts.Table, condition)

	body := map[string]interface{}{
		"variables": map[string]interface{}{},
		"query":     mutation,
	}

	result, statusCode, err := c.RawRequest(ctx, "POST", "/api/now/graphql", body, nil)
	if err != nil {
		return fmt.Errorf("failed to execute GraphQL mutation: %w", err)
	}
	if statusCode != 200 {
		return fmt.Errorf("GraphQL request failed with status %d", statusCode)
	}

	// Check for GraphQL errors in the response
	if resultMap, ok := result.(map[string]interface{}); ok {
		if errors, hasErrors := resultMap["errors"]; hasErrors {
			if errList, ok := errors.([]interface{}); ok && len(errList) > 0 {
				if firstErr, ok := errList[0].(map[string]interface{}); ok {
					return fmt.Errorf("GraphQL error: %s", getString(firstErr, "message"))
				}
			}
		}
	}

	return nil
}

// getCurrentUserSysID returns the sys_id of the currently authenticated user.
func (c *Client) getCurrentUserSysID(ctx context.Context) (string, error) {
	resp, err := c.Get(ctx, "sys_user", url.Values{
		"sysparm_query":  []string{"sys_id=javascript:gs.getUserID()"},
		"sysparm_limit":  []string{"1"},
		"sysparm_fields": []string{"sys_id"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to query current user: %w", err)
	}
	if len(resp.Result) == 0 {
		return "", fmt.Errorf("could not determine current user")
	}
	return getString(resp.Result[0], "sys_id"), nil
}

// buildTriggerInsertMutation builds the GraphQL mutation string for inserting a record trigger.
// This matches the exact format used by Flow Designer's UI.
func buildTriggerInsertMutation(flowSysID, triggerName, triggerDefID, triggerType, table, condition string) string {
	// The condition value needs ^EQ appended if empty (Flow Designer convention)
	conditionValue := condition
	if conditionValue == "" {
		conditionValue = "^EQ"
	}

	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Record", triggerDefinitionId: "%s", type: "%s", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "table", label: "Table", internalType: "table_name", mandatory: true, order: 1, valueSysId: "", field_name: "table", type: "table_name", children: [], displayValue: {value: ""}, value: {value: "%s"}, parameter: {id: "cfca92e0c31322002841b63b12d3ae00", label: "Table", name: "table", type: "table_name", type_label: "Table Name", hint: "", order: 1, extended: false, mandatory: true, readonly: false, maxsize: 80, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "filter_table_source=RECORD_WATCHER_RESTRICTED,", sys_class_name: "", children: []}}, {name: "condition", label: "Condition", internalType: "conditions", mandatory: false, order: 100, valueSysId: "", field_name: "condition", type: "conditions", children: [], displayValue: {value: ""}, value: {schemaless: false, schemalessValue: "", value: "%s"}, parameter: {id: "66aadea0c31322002841b63b12d3aebf", label: "Condition", name: "condition", type: "conditions", type_label: "Conditions", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 4000, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: true, dependent_on: "table", internal_link: "", show_ref_finder: false, local: false, attributes: "modelDependent=trigger_inputs,wants_to_add_conditions=true,", sys_class_name: "", children: []}}, {name: "run_on_extended", label: "run_on_extended", internalType: "choice", mandatory: false, order: 100, valueSysId: "", field_name: "run_on_extended", type: "choice", children: [], choiceList: [{label: "Run only on current table", value: "false"}, {label: "Run on current and extended tables", value: "true"}], displayValue: {value: ""}, value: {schemaless: false, schemalessValue: "", value: "false"}, parameter: {id: "11ffbef2072200103bf10705afd300c2", label: "run_on_extended", name: "run_on_extended", type: "choice", type_label: "Choice", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "3", table: "", columnName: "", defaultValue: "false", defaultDisplayValue: "Run only on current table", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "advanced=true,", sys_class_name: "", choices: [{label: "Run only on current table", order: 0, value: "false"}, {label: "Run on current and extended tables", order: 1, value: "true"}], defaultChoices: [{label: "Run only on current table", order: 1, value: "false"}, {label: "Run on current and extended tables", order: 2, value: "true"}], children: []}}, {name: "run_flow_in", label: "run_flow_in", internalType: "choice", mandatory: false, order: 100, valueSysId: "", field_name: "run_flow_in", type: "choice", children: [], choiceList: [{label: "Run flow in background (default)", value: "background"}, {label: "Run flow in foreground", value: "foreground"}], displayValue: {value: ""}, value: {schemaless: false, schemalessValue: "", value: "any"}, parameter: {id: "3f1b9e4e0f103300b599bca2ff767e21", label: "run_flow_in", name: "run_flow_in", type: "choice", type_label: "Choice", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "3", table: "", columnName: "", defaultValue: "any", defaultDisplayValue: "any", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "advanced=true,", sys_class_name: "", choices: [{label: "Run flow in background (default)", order: 0, value: "background"}, {label: "Run flow in foreground", order: 1, value: "foreground"}], defaultChoices: [{label: "Run flow in background (default)", order: 1, value: "background"}, {label: "Run flow in foreground", order: 2, value: "foreground"}], children: []}}, {name: "run_when_user_list", label: "run_when_user_list", internalType: "glide_list", mandatory: false, order: 100, valueSysId: "", field_name: "run_when_user_list", type: "glide_list", children: [], displayValue: {value: ""}, value: {value: ""}, parameter: {id: "f89c5177c7002300f4eba1425a976385", label: "run_when_user_list", name: "run_when_user_list", type: "glide_list", type_label: "List", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 4000, data_structure: "", reference: "sys_user", reference_display: "User", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "advanced=true,", sys_class_name: "", children: []}}, {name: "run_when_setting", label: "run_when_setting", internalType: "choice", mandatory: false, order: 100, valueSysId: "", field_name: "run_when_setting", type: "choice", children: [], choiceList: [{label: "Only Run for Non-Interactive Session", value: "non_interactive"}, {label: "Only Run for User Interactive Session", value: "interactive"}, {label: "Run for Both Interactive and Non-Interactive Sessions", value: "both"}], displayValue: {value: ""}, value: {schemaless: false, schemalessValue: "", value: "both"}, parameter: {id: "1e4859f3c7002300f4eba1425a9763f9", label: "run_when_setting", name: "run_when_setting", type: "choice", type_label: "Choice", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "3", table: "", columnName: "", defaultValue: "both", defaultDisplayValue: "Run for Both Interactive and Non-Interactive Sessions", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "advanced=true,", sys_class_name: "", choices: [{label: "Only Run for Non-Interactive Session", order: 0, value: "non_interactive"}, {label: "Only Run for User Interactive Session", order: 1, value: "interactive"}, {label: "Run for Both Interactive and Non-Interactive Sessions", order: 2, value: "both"}], defaultChoices: [{label: "Only Run for Non-Interactive Session", order: 1, value: "non_interactive"}, {label: "Only Run for User Interactive Session", order: 2, value: "interactive"}, {label: "Run for Both Interactive and Non-Interactive Sessions", order: 3, value: "both"}], children: []}}, {name: "run_when_user_setting", label: "run_when_user_setting", internalType: "choice", mandatory: false, order: 100, valueSysId: "", field_name: "run_when_user_setting", type: "choice", children: [], choiceList: [{label: "Do not run if triggered by the following users", value: "not_one_of"}, {label: "Only Run if triggered by the following users", value: "one_of"}, {label: "Run for any user", value: "any"}], displayValue: {value: ""}, value: {schemaless: false, schemalessValue: "", value: "any"}, parameter: {id: "ed7a5537c7002300f4eba1425a976391", label: "run_when_user_setting", name: "run_when_user_setting", type: "choice", type_label: "Choice", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "3", table: "", columnName: "", defaultValue: "any", defaultDisplayValue: "Run for any user", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "advanced=true,", sys_class_name: "", choices: [{label: "Do not run if triggered by the following users", order: 0, value: "not_one_of"}, {label: "Only Run if triggered by the following users", order: 1, value: "one_of"}, {label: "Run for any user", order: 2, value: "any"}], defaultChoices: [{label: "Do not run if triggered by the following users", order: 1, value: "not_one_of"}, {label: "Only Run if triggered by the following users", order: 2, value: "one_of"}, {label: "Run for any user", order: 3, value: "any"}], children: []}}], outputs: [{name: "current", value: "", displayValue: "", type: "document_id", order: 100, label: "Record", children: [], parameter: {id: "1e9b880ec37432002841b63b12d3ae89", label: "Record", name: "current", type: "document_id", type_label: "Document ID", hint: "", order: 100, extended: false, mandatory: true, readonly: false, maxsize: 200, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: true, dependent_on: "table_name", internal_link: "table", show_ref_finder: false, local: false, attributes: "", sys_class_name: ""}}, {name: "table_name", value: "", displayValue: "", type: "table_name", order: 101, label: "Table Name", children: [], parameter: {id: "42aa40cac33432002841b63b12d3aea6", label: "Table Name", name: "table_name", type: "table_name", type_label: "Table Name", hint: "", order: 101, extended: false, mandatory: false, readonly: false, maxsize: 200, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "table", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Time UTC", children: [], parameter: {id: "788cb3f2c33332002841b63b12d3ae6a", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 200, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Date/Time", children: [], parameter: {id: "bdd0520777133010ecf06097bd5a9918", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 200, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, triggerType, table, conditionValue)
}

// resolveFlowID takes a flow identifier (sys_id or name) and returns the sys_id.
func (c *Client) resolveFlowID(ctx context.Context, flowID string) (string, error) {
	// Try by sys_id first if it looks like one (32 hex chars)
	if len(flowID) == 32 {
		resp, err := c.Get(ctx, "sys_hub_flow", url.Values{
			"sysparm_query":  []string{fmt.Sprintf("sys_id=%s", flowID)},
			"sysparm_limit":  []string{"1"},
			"sysparm_fields": []string{"sys_id"},
		})
		if err == nil && len(resp.Result) > 0 {
			return getString(resp.Result[0], "sys_id"), nil
		}
	}

	// Try by name
	resp, err := c.Get(ctx, "sys_hub_flow", url.Values{
		"sysparm_query":  []string{fmt.Sprintf("name=%s", flowID)},
		"sysparm_limit":  []string{"1"},
		"sysparm_fields": []string{"sys_id"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to query flow: %w", err)
	}
	if len(resp.Result) == 0 {
		return "", fmt.Errorf("flow not found: %s", flowID)
	}
	return getString(resp.Result[0], "sys_id"), nil
}

// CreateScheduledTriggerOptions holds options for creating a scheduled trigger.
type CreateScheduledTriggerOptions struct {
	FlowID   string // Flow sys_id or name
	Schedule string // "daily", "weekly", "monthly", "once", "repeat"
	Time     string // Time to run (e.g., "08:00:00")
	Day      string // For weekly: day of week ("1"-"7", 1=Mon); for monthly: day of month ("1"-"31")
	Date     string // For run once: full datetime (e.g., "2026-04-16 06:10:00")
	Repeat   string // For repeat: duration as datetime (e.g., "1970-01-05 02:00:01" = 4d 2h 1s interval)
}

// CreateScheduledTrigger creates a scheduled trigger for a flow using the GraphQL API.
// Each trigger type uses its own specific single INSERT mutation with type-specific inputs
// and parameter IDs, matching exactly what Flow Designer's UI sends.
func (c *Client) CreateScheduledTrigger(ctx context.Context, opts CreateScheduledTriggerOptions) error {
	if opts.FlowID == "" {
		return fmt.Errorf("flow ID is required")
	}
	if opts.Schedule == "" {
		return fmt.Errorf("schedule type is required")
	}

	triggerDefID, ok := triggerDefinitionIDs[opts.Schedule]
	if !ok {
		return fmt.Errorf("unsupported schedule type: %s", opts.Schedule)
	}

	flowSysID, err := c.resolveFlowID(ctx, opts.FlowID)
	if err != nil {
		return fmt.Errorf("failed to resolve flow: %w", err)
	}

	// Get current user for safe edit lock
	userSysID, err := c.getCurrentUserSysID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	// Acquire safe edit lock
	safeEditResp, err := c.Post(ctx, "sys_hub_flow_safe_edit", map[string]interface{}{
		"flow": flowSysID,
		"user": userSysID,
	})
	if err != nil {
		return fmt.Errorf("failed to acquire safe edit lock: %w", err)
	}
	safeEditID := getString(safeEditResp.Result, "sys_id")
	defer func() {
		if safeEditID != "" {
			_ = c.Delete(ctx, "sys_hub_flow_safe_edit", safeEditID)
		}
	}()

	// Format time value as "1970-01-01 HH:MM:SS" (daily, weekly, monthly need this)
	timeValue := opts.Time
	if timeValue == "" {
		timeValue = "08:00:00"
	}
	if !strings.Contains(timeValue, " ") {
		timeValue = "1970-01-01 " + timeValue
	}

	// Build INSERT mutation — each type has its own builder with type-specific param IDs
	triggerName := getTriggerName(opts.Schedule)
	var insertMutation string

	switch opts.Schedule {
	case "daily":
		insertMutation = buildDailyTriggerInsertMutation(flowSysID, triggerName, triggerDefID, timeValue)
	case "repeat":
		if opts.Repeat == "" {
			return fmt.Errorf("repeat interval is required for repeat schedule")
		}
		insertMutation = buildRepeatTriggerInsertMutation(flowSysID, triggerName, triggerDefID, opts.Repeat)
	case "weekly":
		dayValue := opts.Day
		if dayValue == "" {
			dayValue = "2" // Default: Tuesday
		}
		insertMutation = buildWeeklyTriggerInsertMutation(flowSysID, triggerName, triggerDefID, dayValue, timeValue)
	case "monthly":
		dayValue := opts.Day
		if dayValue == "" {
			dayValue = "1" // Default: 1st of month
		}
		insertMutation = buildMonthlyTriggerInsertMutation(flowSysID, triggerName, triggerDefID, dayValue, timeValue)
	case "once":
		if opts.Date == "" {
			return fmt.Errorf("date is required for run-once schedule")
		}
		insertMutation = buildOnceTriggerInsertMutation(flowSysID, triggerName, triggerDefID, opts.Date)
	default:
		return fmt.Errorf("unsupported schedule type: %s", opts.Schedule)
	}

	body := map[string]interface{}{
		"variables": map[string]interface{}{},
		"query":     insertMutation,
	}

	result, statusCode, err := c.RawRequest(ctx, "POST", "/api/now/graphql", body, nil)
	if err != nil {
		return fmt.Errorf("failed to execute GraphQL mutation: %w", err)
	}
	if statusCode != 200 {
		return fmt.Errorf("GraphQL request failed with status %d", statusCode)
	}

	// Check for GraphQL errors
	if resultMap, ok := result.(map[string]interface{}); ok {
		if errors, hasErrors := resultMap["errors"]; hasErrors {
			if errList, ok := errors.([]interface{}); ok && len(errList) > 0 {
				if firstErr, ok := errList[0].(map[string]interface{}); ok {
					return fmt.Errorf("GraphQL error: %s", getString(firstErr, "message"))
				}
			}
		}
	}

	return nil
}

// buildDailyTriggerInsertMutation builds the GraphQL mutation for inserting a daily trigger.
// Uses daily-specific parameter IDs for time input and outputs.
func buildDailyTriggerInsertMutation(flowSysID, triggerName, triggerDefID, timeValue string) string {
	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Scheduled", triggerDefinitionId: "%s", type: "daily", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "time", label: "Time", internalType: "glide_time", mandatory: true, order: 1, valueSysId: "", field_name: "time", type: "glide_time", children: [], displayValue: {value: ""}, value: {value: "%s"}, parameter: {id: "3ad4edc0c32222002841b63b12d3aee5", label: "Time", name: "time", type: "glide_time", type_label: "Time", hint: "", order: 1, extended: false, mandatory: true, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "", sys_class_name: "", children: []}}], outputs: [{name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Date/Time", children: [], parameter: {id: "b270dac377133010ecf06097bd5a998e", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 200, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Time UTC", children: [], parameter: {id: "4d0472e0c32132002841b63b12d3ae68", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 200, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, timeValue)
}

// buildRepeatTriggerInsertMutation builds the GraphQL mutation for inserting a repeat trigger.
// Unlike other scheduled triggers, repeat triggers include the repeat duration directly in the INSERT
// (not as a separate UPDATE step). The repeat value is a glide_duration encoded as a datetime
// since epoch (e.g., "1970-01-05 12:00:00" = 4 days, 12 hours).
func buildRepeatTriggerInsertMutation(flowSysID, triggerName, triggerDefID, repeatValue string) string {
	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Scheduled", triggerDefinitionId: "%s", type: "repeat", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "repeat", label: "Repeat", internalType: "glide_duration", mandatory: true, order: 100, valueSysId: "", field_name: "repeat", type: "glide_duration", children: [], displayValue: {value: ""}, value: {schemaless: false, schemalessValue: "", value: "%s"}, parameter: {id: "1c5f4d94c32222002841b63b12d3aeb3", label: "Repeat", name: "repeat", type: "glide_duration", type_label: "Duration", hint: "", order: 100, extended: false, mandatory: true, readonly: false, maxsize: 100, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "element_mapping_provider=com.glide.flow_design.action.data.FlowDesignVariableMapper,", sys_class_name: "", children: []}}], outputs: [{name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Date/Time", children: [], parameter: {id: "0af0d20777133010ecf06097bd5a994e", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Time UTC", children: [], parameter: {id: "795436e0c32132002841b63b12d3aea9", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, repeatValue)
}

// buildWeeklyTriggerInsertMutation builds the GraphQL mutation for inserting a weekly trigger.
// Includes both day_of_week and time inputs with weekly-specific parameter IDs.
func buildWeeklyTriggerInsertMutation(flowSysID, triggerName, triggerDefID, dayOfWeek, timeValue string) string {
	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Scheduled", triggerDefinitionId: "%s", type: "weekly", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "day_of_week", label: "Day of Week", internalType: "day_of_week", mandatory: true, order: 10, valueSysId: "", field_name: "day_of_week", type: "day_of_week", children: [], displayValue: {value: ""}, value: {value: "%s"}, parameter: {id: "c685a104c32222002841b63b12d3aed3", label: "Day of Week", name: "day_of_week", type: "day_of_week", type_label: "Day of Week", hint: "", order: 10, extended: false, mandatory: true, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "element_mapping_provider=com.glide.flow_design.action.data.FlowDesignVariableMapper,", sys_class_name: "", children: []}}, {name: "time", label: "Time", internalType: "glide_time", mandatory: true, order: 100, valueSysId: "", field_name: "time", type: "glide_time", children: [], displayValue: {schemaless: false, schemalessValue: "", value: "%s"}, value: {schemaless: false, schemalessValue: "", value: "%s"}, parameter: {id: "a745a104c32222002841b63b12d3ae18", label: "Time", name: "time", type: "glide_time", type_label: "Time", hint: "", order: 100, extended: false, mandatory: true, readonly: false, maxsize: 100, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "element_mapping_provider=com.glide.flow_design.action.data.FlowDesignVariableMapper,", sys_class_name: "", children: []}}], outputs: [{name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Date/Time", children: [], parameter: {id: "1c815a0777133010ecf06097bd5a9952", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Time UTC", children: [], parameter: {id: "b534b2e0c32132002841b63b12d3aefa", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, dayOfWeek, timeValue, timeValue)
}

// buildMonthlyTriggerInsertMutation builds the GraphQL mutation for inserting a monthly trigger.
// Includes both day_of_month and time inputs with monthly-specific parameter IDs.
func buildMonthlyTriggerInsertMutation(flowSysID, triggerName, triggerDefID, dayOfMonth, timeValue string) string {
	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Scheduled", triggerDefinitionId: "%s", type: "monthly", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "day_of_month", label: "Day of Month", internalType: "integer", mandatory: true, order: 10, valueSysId: "", field_name: "day_of_month", type: "integer", children: [], displayValue: {value: ""}, value: {value: "%s"}, parameter: {id: "ab36a504c32222002841b63b12d3ae8a", label: "Day of Month", name: "day_of_month", type: "integer", type_label: "Integer", hint: "", order: 10, extended: false, mandatory: true, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "element_mapping_provider=com.glide.flow_design.action.data.FlowDesignVariableMapper,integer_type=day_of_month,", sys_class_name: "", children: []}}, {name: "time", label: "Time", internalType: "glide_time", mandatory: true, order: 100, valueSysId: "", field_name: "time", type: "glide_time", children: [], displayValue: {schemaless: false, schemalessValue: "", value: "%s"}, value: {schemaless: false, schemalessValue: "", value: "%s"}, parameter: {id: "b5d52504c32222002841b63b12d3aeaa", label: "Time", name: "time", type: "glide_time", type_label: "Time", hint: "", order: 100, extended: false, mandatory: true, readonly: false, maxsize: 100, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "element_mapping_provider=com.glide.flow_design.action.data.FlowDesignVariableMapper,", sys_class_name: "", children: []}}], outputs: [{name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Date/Time", children: [], parameter: {id: "c2bf828377133010ecf06097bd5a9944", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Time UTC", children: [], parameter: {id: "2544f2e0c32132002841b63b12d3aedc", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, dayOfMonth, timeValue, timeValue)
}

// buildOnceTriggerInsertMutation builds the GraphQL mutation for inserting a run-once trigger.
// Note: the GraphQL type is "run_once" (not "once").
func buildOnceTriggerInsertMutation(flowSysID, triggerName, triggerDefID, dateTimeValue string) string {
	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Scheduled", triggerDefinitionId: "%s", type: "run_once", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "run_in", label: "Run on", internalType: "glide_date_time", mandatory: true, order: 100, valueSysId: "", field_name: "run_in", type: "glide_date_time", children: [], displayValue: {value: ""}, value: {value: "%s"}, parameter: {id: "92afcd94c32222002841b63b12d3aee8", label: "Run on", name: "run_in", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 100, extended: false, mandatory: true, readonly: false, maxsize: 100, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "element_mapping_provider=com.glide.flow_design.action.data.FlowDesignVariableMapper,", sys_class_name: "", children: []}}], outputs: [{name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Date/Time", children: [], parameter: {id: "eda09ec377133010ecf06097bd5a99be", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 110, label: "Run Start Time UTC", children: [], parameter: {id: "4fd37ea0c32132002841b63b12d3ae07", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 110, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, dateTimeValue)
}

// CreateApplicationTriggerOptions holds options for creating an application trigger.
type CreateApplicationTriggerOptions struct {
	FlowID      string // Flow sys_id or name
	Application string // "service_catalog"
}

// CreateApplicationTrigger creates an application trigger for a flow using the GraphQL API.
// Currently supports "service_catalog" triggers only.
func (c *Client) CreateApplicationTrigger(ctx context.Context, opts CreateApplicationTriggerOptions) error {
	if opts.FlowID == "" {
		return fmt.Errorf("flow ID is required")
	}
	if opts.Application == "" {
		return fmt.Errorf("application is required")
	}

	triggerDefID, ok := triggerDefinitionIDs[opts.Application]
	if !ok {
		return fmt.Errorf("unsupported application trigger: %s", opts.Application)
	}

	flowSysID, err := c.resolveFlowID(ctx, opts.FlowID)
	if err != nil {
		return fmt.Errorf("failed to resolve flow: %w", err)
	}

	// Get current user for safe edit lock
	userSysID, err := c.getCurrentUserSysID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	// Acquire safe edit lock
	safeEditResp, err := c.Post(ctx, "sys_hub_flow_safe_edit", map[string]interface{}{
		"flow": flowSysID,
		"user": userSysID,
	})
	if err != nil {
		return fmt.Errorf("failed to acquire safe edit lock: %w", err)
	}
	safeEditID := getString(safeEditResp.Result, "sys_id")
	defer func() {
		if safeEditID != "" {
			_ = c.Delete(ctx, "sys_hub_flow_safe_edit", safeEditID)
		}
	}()

	// Build and send GraphQL mutation
	triggerName := getTriggerName(opts.Application)
	mutation := buildApplicationTriggerInsertMutation(flowSysID, triggerName, triggerDefID, opts.Application)

	body := map[string]interface{}{
		"variables": map[string]interface{}{},
		"query":     mutation,
	}

	result, statusCode, err := c.RawRequest(ctx, "POST", "/api/now/graphql", body, nil)
	if err != nil {
		return fmt.Errorf("failed to execute GraphQL mutation: %w", err)
	}
	if statusCode != 200 {
		return fmt.Errorf("GraphQL request failed with status %d", statusCode)
	}

	// Check for GraphQL errors
	if resultMap, ok := result.(map[string]interface{}); ok {
		if errors, hasErrors := resultMap["errors"]; hasErrors {
			if errList, ok := errors.([]interface{}); ok && len(errList) > 0 {
				if firstErr, ok := errList[0].(map[string]interface{}); ok {
					return fmt.Errorf("GraphQL error: %s", getString(firstErr, "message"))
				}
			}
		}
	}

	return nil
}

// buildApplicationTriggerInsertMutation builds the GraphQL mutation for inserting an application trigger.
// Currently supports service_catalog which outputs request_item, run_start_time, run_start_date_time, table_name.
func buildApplicationTriggerInsertMutation(flowSysID, triggerName, triggerDefID, appType string) string {
	return fmt.Sprintf(`mutation {
  global {
    snFlowDesigner {
      flow(flowPatch: {flowId: "%s", triggerInstances: {insert: [{flowSysId: "%s", name: "%s", triggerType: "Application", triggerDefinitionId: "%s", type: "%s", hasDynamicOutputs: false, metadata: "{\"predicates\":[]}", inputs: [{name: "run_flow_in", label: "Run Flow In", internalType: "choice", mandatory: false, order: 100, valueSysId: "", field_name: "run_flow_in", type: "choice", children: [], choiceList: [{label: "Run flow in background (default)", value: "background"}, {label: "Run flow in foreground", value: "foreground"}], displayValue: {value: ""}, value: {value: ""}, parameter: {id: "6090c72977203300f5bfcfcc78106159", label: "Run Flow In", name: "run_flow_in", type: "choice", type_label: "Choice", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "3", table: "sys_flow_record_trigger", columnName: "run_flow_in", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "advanced=true,", sys_class_name: "", choices: [{label: "Run flow in background (default)", order: 0, value: "background"}, {label: "Run flow in foreground", order: 1, value: "foreground"}], defaultChoices: [{label: "Run flow in background (default)", order: 1, value: "background"}, {label: "Run flow in foreground", order: 2, value: "foreground"}], children: []}}], outputs: [{name: "request_item", value: "", displayValue: "", type: "reference", order: 100, label: "Request Item", children: [], parameter: {id: "002f9851c36813002841b63b12d3ae1a", label: "Request Item", name: "request_item", type: "reference", type_label: "Reference", hint: "", order: 100, extended: false, mandatory: true, readonly: false, maxsize: 32, data_structure: "", reference: "sc_req_item", reference_display: "Requested Item", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, searchField: "number", attributes: "default_search_field=number,", sys_class_name: ""}}, {name: "run_start_time", value: "", displayValue: "", type: "glide_date_time", order: 100, label: "Run Start Time UTC", children: [], parameter: {id: "035e9451c36813002841b63b12d3aef0", label: "Run Start Time UTC", name: "run_start_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "run_start_date_time", value: "", displayValue: "", type: "glide_date_time", order: 100, label: "Run Start Date/Time", children: [], parameter: {id: "c50056c377133010ecf06097bd5a999c", label: "Run Start Date/Time", name: "run_start_date_time", type: "glide_date_time", type_label: "Date/Time", hint: "", order: 100, extended: false, mandatory: false, readonly: false, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}, {name: "table_name", value: "sc_req_item", displayValue: "", type: "table_name", order: 100, label: "Table Name", children: [], parameter: {id: "34de1851c36813002841b63b12d3ae43", label: "Table Name", name: "table_name", type: "table_name", type_label: "Table Name", hint: "", order: 100, extended: false, mandatory: false, readonly: true, maxsize: 40, data_structure: "", reference: "", reference_display: "", ref_qual: "", choiceOption: "", table: "", columnName: "", defaultValue: "sc_req_item", defaultDisplayValue: "sc_req_item", use_dependent: false, dependent_on: "", internal_link: "", show_ref_finder: false, local: false, attributes: "test_input_hidden=true,", sys_class_name: ""}}]}]}}) {
        id
        triggerInstances {
          inserts {
            sysId
            uiUniqueIdentifier
            __typename
          }
          updates
          deletes
          __typename
        }
        __typename
      }
      __typename
    }
    __typename
  }
}`, flowSysID, flowSysID, triggerName, triggerDefID, appType)
}

// getTriggerName returns the display name for a trigger type.
func getTriggerName(triggerType string) string {
	switch triggerType {
	case "record_create":
		return "Created"
	case "record_update":
		return "Updated"
	case "record_create_or_update":
		return "Created or Updated"
	case "daily":
		return "Daily"
	case "weekly":
		return "Weekly"
	case "monthly":
		return "Monthly"
	case "once":
		return "Run Once"
	case "repeat":
		return "Repeat"
	case "service_catalog":
		return "Service Catalog"
	default:
		return triggerType
	}
}
