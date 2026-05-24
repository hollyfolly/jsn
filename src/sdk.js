// ServiceNow REST API client

import { errAuth, errAPI, errNetwork } from './errors.js';
import { getStringField } from './helpers.js';

const DEFAULT_TIMEOUT = 30000;

export class SDKClient {
  constructor(baseURL, authProvider, opts = {}) {
    this.baseURL = baseURL.replace(/\/$/, '');
    this.authProvider = authProvider;
    this.timeout = opts.timeout || DEFAULT_TIMEOUT;
  }

  async _setAuth(req) {
    if (!this.authProvider) {
      throw errAuth('No authentication configured');
    }
    const creds = await this.authProvider.getCredentials();
    if (!creds) {
      throw errAuth('No valid credentials');
    }
    switch (creds.auth_method) {
      case 'basic':
        req.headers.set('Authorization', 'Basic ' + Buffer.from(`${creds.username}:${creds.password}`).toString('base64'));
        break;
      case 'token':
      case 'oauth':
        req.headers.set('Authorization', `Bearer ${creds.access_token}`);
        break;
      default:
        if (creds.username && creds.password) {
          req.headers.set('Authorization', 'Basic ' + Buffer.from(`${creds.username}:${creds.password}`).toString('base64'));
        } else if (creds.access_token) {
          req.headers.set('Authorization', `Bearer ${creds.access_token}`);
        } else {
          throw errAuth('No valid credentials');
        }
    }
  }

  async request(endpoint, opts = {}) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);

    try {
      const req = new Request(endpoint, {
        ...opts,
        signal: controller.signal,
      });
      req.headers.set('Accept', 'application/json');
      if (opts.body && typeof opts.body === 'string') {
        req.headers.set('Content-Type', 'application/json');
      }
      await this._setAuth(req);

      const resp = await fetch(req);
      const body = await resp.text();

      if (!resp.ok) {
        throw errAPI(resp.status, body || resp.statusText);
      }

      if (resp.status === 204 || body === '') {
        return null;
      }

      return JSON.parse(body);
    } catch (err) {
      if (err.name === 'AbortError') {
        throw errNetwork(new Error('Request timed out'));
      }
      if (err.code === 'ECONNREFUSED' || err.code === 'ENOTFOUND' || err.code === 'ETIMEDOUT') {
        throw errNetwork(err);
      }
      throw err;
    } finally {
      clearTimeout(timer);
    }
  }

  async list(table, params = {}) {
    const query = new URLSearchParams(params).toString();
    const endpoint = `${this.baseURL}/api/now/table/${table}${query ? '?' + query : ''}`;
    const result = await this.request(endpoint, { method: 'GET' });
    return result?.result || [];
  }

  async get(table, sysID) {
    const endpoint = `${this.baseURL}/api/now/table/${table}/${sysID}`;
    const result = await this.request(endpoint, { method: 'GET' });
    return result?.result || null;
  }

  async create(table, data) {
    const endpoint = `${this.baseURL}/api/now/table/${table}`;
    const result = await this.request(endpoint, {
      method: 'POST',
      body: JSON.stringify(data),
    });
    return result?.result || null;
  }

  async update(table, sysID, data) {
    const endpoint = `${this.baseURL}/api/now/table/${table}/${sysID}`;
    const result = await this.request(endpoint, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
    return result?.result || null;
  }

  async delete(table, sysID) {
    const endpoint = `${this.baseURL}/api/now/table/${table}/${sysID}`;
    await this.request(endpoint, { method: 'DELETE' });
  }

  async inspectFlow(identifier) {
    const isSysID = identifier.length === 32 && /^[0-9a-fA-F]+$/.test(identifier);

    // 1) Resolve flow
    const flowQuery = new URLSearchParams();
    flowQuery.set('sysparm_display_value', 'all');
    flowQuery.set('sysparm_limit', '1');
    flowQuery.set('sysparm_query', isSysID ? `sys_id=${identifier}` : `name=${identifier}`);
    flowQuery.set('sysparm_fields', 'sys_id,name,active,version,type');

    const flowRecords = await this.list('sys_hub_flow', flowQuery);
    if (!flowRecords || flowRecords.length === 0) {
      const err = new Error(`flow not found: ${identifier}`);
      err.code = 'not_found';
      throw err;
    }

    const flow = flowRecords[0];
    const flowSysID = getStringField(flow, 'sys_id');
    let flowVersion = getStringField(flow, 'version');

    const inspection = {
      flow: {
        name: getStringField(flow, 'name'),
        active: getBoolField(flow, 'active'),
        version: flowVersion,
        type: getStringField(flow, 'type'),
        sysID: flowSysID,
      },
      version: {},
      payload: {},
      triggerInstances: [],
      actionInstances: [],
      flowLogicInstances: [],
      subFlowInstances: [],
      flowInputs: [],
      flowOutputs: [],
    };

    // 2) Fetch latest flow version
    const versionQuery = new URLSearchParams();
    versionQuery.set('sysparm_display_value', 'all');
    versionQuery.set('sysparm_limit', '1');
    versionQuery.set('sysparm_query', `flow=${flowSysID}^ORDERBYDESCsys_updated_on`);
    versionQuery.set('sysparm_fields', 'sys_id,flow,version,payload,sys_updated_on');

    const versionRecords = await this.list('sys_hub_flow_version', versionQuery);
    if (versionRecords && versionRecords.length > 0) {
      inspection.version = versionRecords[0];
      if (!inspection.flow.version) {
        inspection.flow.version = getStringField(versionRecords[0], 'version');
      }

      const payload = getStringField(versionRecords[0], 'payload');
      if (payload) {
        try {
          const payloadData = JSON.parse(payload);
          inspection.payload = payloadData;
          extractPayloadData(inspection, payloadData);
        } catch {
          // ignore parse error
        }
      }
    }

    // 3) Fetch trigger instances
    const triggerQuery = new URLSearchParams();
    triggerQuery.set('sysparm_display_value', 'all');
    triggerQuery.set('sysparm_query', `flow=${flowSysID}`);
    triggerQuery.set('sysparm_fields', 'sys_id,name,trigger_type,display_text,active,trigger_definition');
    triggerQuery.set('sysparm_limit', '20');
    const triggerRecords = await this.list('sys_hub_trigger_instance', triggerQuery);
    if (triggerRecords) {
      inspection.triggerInstances = triggerRecords;
    }

    // 4) Fallbacks when payload did not provide structure arrays
    if (!inspection.actionInstances || inspection.actionInstances.length === 0) {
      const actionQuery = new URLSearchParams();
      actionQuery.set('sysparm_display_value', 'all');
      actionQuery.set('sysparm_query', `flow=${flowSysID}^ORDERBYorder`);
      actionQuery.set('sysparm_fields', 'sys_id,order,name,display_text,comment,action_type');
      actionQuery.set('sysparm_limit', '200');
      const records = await this.list('sys_hub_action_instance', actionQuery);
      if (records) inspection.actionInstances = records;
    }

    if (!inspection.flowLogicInstances || inspection.flowLogicInstances.length === 0) {
      const logicTables = ['sys_hub_flow_logic', 'sys_hub_flow_logic_instance_v2'];
      for (const table of logicTables) {
        const logicQuery = new URLSearchParams();
        logicQuery.set('sysparm_display_value', 'all');
        logicQuery.set('sysparm_query', `flow=${flowSysID}^ORDERBYorder`);
        logicQuery.set('sysparm_fields', 'sys_id,order,name,display_text,comment,parent_ui_id,logic_definition');
        logicQuery.set('sysparm_limit', '200');
        const records = await this.list(table, logicQuery);
        if (records) inspection.flowLogicInstances.push(...records);
      }
      inspection.flowLogicInstances.sort((a, b) => parseOrderField(a) - parseOrderField(b));
    }

    if (!inspection.subFlowInstances || inspection.subFlowInstances.length === 0) {
      const subflowQuery = new URLSearchParams();
      subflowQuery.set('sysparm_display_value', 'all');
      subflowQuery.set('sysparm_query', `flow=${flowSysID}^ORDERBYorder`);
      subflowQuery.set('sysparm_fields', 'sys_id,order,subflow,name,display_text,comment,parent_ui_id');
      subflowQuery.set('sysparm_limit', '200');
      const records = await this.list('sys_hub_sub_flow_instance', subflowQuery);
      if (records) inspection.subFlowInstances = records;
    }

    // Inputs/Outputs fallback
    if (!inspection.flowInputs || inspection.flowInputs.length === 0) {
      const inputsQuery = new URLSearchParams();
      inputsQuery.set('sysparm_display_value', 'all');
      inputsQuery.set('sysparm_query', `model=${flowSysID}^ORDERBYorder`);
      inputsQuery.set('sysparm_fields', 'sys_id,name,label,type,mandatory,order');
      inputsQuery.set('sysparm_limit', '200');
      const records = await this.list('sys_hub_flow_input', inputsQuery);
      if (records) inspection.flowInputs = records;
    }

    if (!inspection.flowOutputs || inspection.flowOutputs.length === 0) {
      const outputsQuery = new URLSearchParams();
      outputsQuery.set('sysparm_display_value', 'all');
      outputsQuery.set('sysparm_query', `model=${flowSysID}^ORDERBYorder`);
      outputsQuery.set('sysparm_fields', 'sys_id,name,label,type,order');
      outputsQuery.set('sysparm_limit', '200');
      const records = await this.list('sys_hub_flow_output', outputsQuery);
      if (records) inspection.flowOutputs = records;
    }

    return inspection;
  }

  async aggregateCount(table, queryStr) {
    const params = new URLSearchParams();
    params.set('sysparm_count', 'true');
    if (queryStr) params.set('sysparm_query', queryStr);
    const endpoint = `${this.baseURL}/api/now/stats/${table}?${params.toString()}`;
    const result = await this.request(endpoint, { method: 'GET' });
    const stats = result?.result?.stats;
    if (!stats) return 0;

    let statsMap = stats;
    if (typeof stats === 'string') {
      try { statsMap = JSON.parse(stats); } catch { return 0; }
    }

    if (statsMap.count != null) {
      const v = statsMap.count;
      if (typeof v === 'number') return v;
      if (typeof v === 'string') {
        const n = parseInt(v, 10);
        return isNaN(n) ? 0 : n;
      }
    }

    for (const value of Object.values(statsMap)) {
      if (value && typeof value === 'object' && value.count != null) {
        const v = value.count;
        if (typeof v === 'number') return v;
        if (typeof v === 'string') {
          const n = parseInt(v, 10);
          return isNaN(n) ? 0 : n;
        }
      }
    }

    return 0;
  }
}

function getBoolField(record, field) {
  const val = record?.[field];
  if (val == null) return false;
  if (typeof val === 'boolean') return val;
  if (typeof val === 'string') return val === 'true' || val === '1';
  if (typeof val === 'object') {
    const dv = val.display_value;
    if (dv != null) return String(dv) === 'true' || String(dv) === '1';
    const v = val.value;
    if (v != null) return String(v) === 'true' || String(v) === '1';
  }
  return false;
}

function extractPayloadData(inspection, payload) {
  const actions = toMapSlice(payload.actionInstances);
  if (actions) inspection.actionInstances = actions;

  const logic = toMapSlice(payload.flowLogicInstances);
  if (logic) inspection.flowLogicInstances = logic;

  const subflows = toMapSlice(payload.subFlowInstances);
  if (subflows) inspection.subFlowInstances = subflows;

  const inputs = toMapSlice(payload.inputs);
  if (inputs) inspection.flowInputs = inputs;

  const outputs = toMapSlice(payload.outputs);
  if (outputs) inspection.flowOutputs = outputs;

  const triggerInstances = payload.triggerInstances;
  if (!Array.isArray(triggerInstances) || triggerInstances.length === 0) return;

  const first = triggerInstances[0];
  if (!first || typeof first !== 'object') return;

  if (!inspection.version || typeof inspection.version !== 'object') {
    inspection.version = {};
  }

  const triggerName = getStringField(first, 'name');
  if (triggerName) inspection.version.trigger_name = triggerName;

  const triggerType = getStringField(first, 'type');
  if (triggerType) inspection.version.trigger_type = triggerType;

  if (Array.isArray(first.inputs)) {
    for (const input of first.inputs) {
      if (!input || typeof input !== 'object') continue;
      const name = getStringField(input, 'name');
      const value = getStringField(input, 'value');
      if (name === 'table' && value) inspection.version.trigger_table = value;
      if (name === 'time' && value) inspection.version.trigger_time = value;
    }
  }
}

function toMapSlice(v) {
  if (!Array.isArray(v)) return null;
  return v.filter(item => item && typeof item === 'object');
}

function parseOrderField(record) {
  const order = getStringField(record, 'order');
  const n = parseInt(order, 10);
  return isNaN(n) ? 0 : n;
}
