import type { Bundle, ZObject } from './types';
import { api, listData } from './client';
import { pollList } from './poll';

// Hidden "list" triggers that power dynamic dropdowns in create/search inputs.
// They never appear as standalone triggers (display.hidden = true). Reference
// them from an input field with e.g. `dynamic: 'campaignList.id.name'`.

export const campaignList = {
  key: 'campaignList',
  noun: 'Campaign',
  display: {
    label: 'Campaign List',
    description: 'Internal trigger that powers campaign dropdowns.',
    hidden: true,
  },
  operation: {
    perform: pollList('/campaigns', {
      map: (c: any) => ({ id: c.id, name: c.name }),
    }),
    sample: { id: '00000000-0000-0000-0000-000000000000', name: 'Q3 Outbound' },
  },
};

export const mailboxList = {
  key: 'mailboxList',
  noun: 'Mailbox',
  display: {
    label: 'Mailbox List',
    description: 'Internal trigger that powers mailbox dropdowns.',
    hidden: true,
  },
  operation: {
    perform: pollList('/emails', {
      map: (m: any) => ({ id: m.id, email: m.email }),
    }),
    sample: { id: '00000000-0000-0000-0000-000000000000', email: 'jane@acme.com' },
  },
};

export const pipelineList = {
  key: 'pipelineList',
  noun: 'Pipeline',
  display: {
    label: 'Pipeline List',
    description: 'Internal trigger that powers CRM pipeline dropdowns.',
    hidden: true,
  },
  operation: {
    // GET /crm/pipelines returns a bare array; pollList handles that.
    perform: pollList('/crm/pipelines', {
      map: (p: any) => ({ id: p.id, name: p.name }),
    }),
    sample: { id: '00000000-0000-0000-0000-000000000000', name: 'Sales' },
  },
};

export const stageList = {
  key: 'stageList',
  noun: 'Stage',
  display: {
    label: 'Pipeline Stage List',
    description: 'Internal trigger that powers CRM stage dropdowns.',
    hidden: true,
  },
  operation: {
    inputFields: [{ key: 'pipeline_id', type: 'string', required: true }],
    perform: async (z: ZObject, bundle: Bundle): Promise<any[]> => {
      const response = await z.request({ url: api('/crm/pipelines') });
      const pipelines: any[] = Array.isArray(response.data) ? response.data : [];
      const pipeline = pipelines.find(
        (p) => p.id === bundle.inputData.pipeline_id,
      );
      const stages: any[] = (pipeline && pipeline.stages) || [];
      return stages.map((s) => ({ id: s.id, name: s.name }));
    },
    sample: { id: '00000000-0000-0000-0000-000000000000', name: 'New' },
  },
};

export const templateList = {
  key: 'templateList',
  noun: 'Template',
  display: {
    label: 'Reply Template List',
    description: 'Internal trigger that powers reply-template dropdowns.',
    hidden: true,
  },
  operation: {
    perform: pollList('/templates', {
      map: (t: any) => ({ id: t.id, name: t.name }),
    }),
    sample: { id: '00000000-0000-0000-0000-000000000000', name: 'Follow up' },
  },
};

export const contactList = {
  key: 'contactList',
  noun: 'Contact',
  display: {
    label: 'Contact List',
    description: 'Internal trigger that powers contact dropdowns.',
    hidden: true,
  },
  operation: {
    perform: async (z: ZObject, _bundle: Bundle): Promise<any[]> => {
      const response = await z.request({
        url: api('/contacts/search'),
        method: 'POST',
        params: { limit: 100 },
        body: { sort_by: 'updated_at' },
      });
      return listData(response).map((c: any) => ({
        id: c.id,
        name:
          c.email ||
          `${c.first_name || ''} ${c.last_name || ''}`.trim() ||
          c.id,
      }));
    },
    sample: { id: '00000000-0000-0000-0000-000000000000', name: 'lead@example.com' },
  },
};

export const dropdownTriggers = [
  campaignList,
  mailboxList,
  pipelineList,
  stageList,
  templateList,
  contactList,
];
