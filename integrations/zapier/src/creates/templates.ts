import type { Bundle, ZObject } from '../types';
import { api, pruneEmpty } from '../lib/client';

export const createTemplate = {
  key: 'createTemplate',
  noun: 'Template',
  display: {
    label: 'Create Reply Template',
    description: 'Creates a reusable reply template for the unified inbox.',
  },
  operation: {
    inputFields: [
      { key: 'name', label: 'Name', type: 'string', required: true },
      { key: 'subject', label: 'Subject', type: 'string' },
      { key: 'body_html', label: 'HTML body', type: 'text' },
      { key: 'body_plain', label: 'Plain-text body', type: 'text' },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const body = pruneEmpty({
        name: bundle.inputData.name,
        subject: bundle.inputData.subject,
        body_html: bundle.inputData.body_html,
        body_plain: bundle.inputData.body_plain,
      });
      const response = await z.request({
        url: api('/templates'),
        method: 'POST',
        body,
      });
      return response.data;
    },
    sample: {
      id: 'tp1a2b3c-0000-0000-0000-000000000000',
      name: 'Follow up',
      subject: 'Following up',
      body_plain: 'Just checking in...',
      position: 1,
      created_at: '2026-06-28T12:00:00Z',
    },
  },
};

export const updateTemplate = {
  key: 'updateTemplate',
  noun: 'Template',
  display: {
    label: 'Update Reply Template',
    description: 'Updates an existing reply template.',
  },
  operation: {
    inputFields: [
      {
        key: 'template_id',
        label: 'Template',
        type: 'string',
        required: true,
        dynamic: 'templateList.id.name',
      },
      { key: 'name', label: 'Name', type: 'string' },
      { key: 'subject', label: 'Subject', type: 'string' },
      { key: 'body_html', label: 'HTML body', type: 'text' },
      { key: 'body_plain', label: 'Plain-text body', type: 'text' },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const body = pruneEmpty({
        name: bundle.inputData.name,
        subject: bundle.inputData.subject,
        body_html: bundle.inputData.body_html,
        body_plain: bundle.inputData.body_plain,
      });
      const response = await z.request({
        url: api(`/templates/${bundle.inputData.template_id}`),
        method: 'PATCH',
        body,
      });
      return response.data;
    },
    sample: {
      id: 'tp1a2b3c-0000-0000-0000-000000000000',
      name: 'Follow up v2',
      subject: 'Following up',
      position: 1,
      updated_at: '2026-06-29T12:00:00Z',
    },
  },
};

export const deleteTemplate = {
  key: 'deleteTemplate',
  noun: 'Template',
  display: {
    label: 'Delete Reply Template',
    description: 'Permanently deletes a reply template by ID.',
  },
  operation: {
    inputFields: [
      {
        key: 'template_id',
        label: 'Template',
        type: 'string',
        required: true,
        dynamic: 'templateList.id.name',
      },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      await z.request({
        url: api(`/templates/${bundle.inputData.template_id}`),
        method: 'DELETE',
      });
      return { id: bundle.inputData.template_id, success: true };
    },
    sample: { id: 'tp1a2b3c-0000-0000-0000-000000000000', success: true },
  },
};
