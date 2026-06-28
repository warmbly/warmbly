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
