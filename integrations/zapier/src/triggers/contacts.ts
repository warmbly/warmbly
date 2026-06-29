import type { Bundle, ZObject } from '../types';
import { api, listData } from '../lib/client';

const CONTACT_SAMPLE = {
  id: '7b6c1f2a-0000-0000-0000-000000000000',
  email: 'lead@example.com',
  first_name: 'Sam',
  last_name: 'Rivera',
  company: 'Example Co',
  phone: '+15551234567',
  subscribed: true,
  custom_fields: { title: 'Head of Growth' },
  categories: [],
  created_at: '2026-06-28T12:00:00Z',
  updated_at: '2026-06-28T12:00:00Z',
};

const CONTACT_OUTPUT = [
  { key: 'id', label: 'Contact ID' },
  { key: 'email', label: 'Email' },
  { key: 'first_name', label: 'First name' },
  { key: 'last_name', label: 'Last name' },
  { key: 'company', label: 'Company' },
  { key: 'phone', label: 'Phone' },
  { key: 'subscribed', type: 'boolean', label: 'Subscribed' },
  { key: 'created_at', label: 'Created at' },
  { key: 'updated_at', label: 'Updated at' },
];

// Search contacts newest-first. `sort_by` must be a bare, whitelisted column
// (created_at | updated_at | ...); direction defaults to DESC, so omitting
// `reverse` returns the most recent rows first. Dedupe is by the returned `id`.
const searchContacts =
  (sortBy: 'created_at' | 'updated_at') =>
  async (z: ZObject, _bundle: Bundle): Promise<any[]> => {
    const response = await z.request({
      url: api('/contacts/search'),
      method: 'POST',
      params: { limit: 100 },
      body: { sort_by: sortBy },
    });
    return listData(response);
  };

export const newContact = {
  key: 'newContact',
  noun: 'Contact',
  display: {
    label: 'New Contact',
    description: 'Triggers when a contact is added to your Warmbly workspace.',
  },
  operation: {
    perform: searchContacts('created_at'),
    sample: CONTACT_SAMPLE,
    outputFields: CONTACT_OUTPUT,
  },
};

export const newOrUpdatedContact = {
  key: 'newOrUpdatedContact',
  noun: 'Contact',
  display: {
    label: 'New or Updated Contact',
    description:
      'Triggers when a contact is created or changed. The real contact id is in the contact_id field; the dedupe id combines it with the update time so edits re-fire.',
  },
  operation: {
    perform: async (z: ZObject, bundle: Bundle): Promise<any[]> => {
      const items = await searchContacts('updated_at')(z, bundle);
      return items.map((c) => ({
        ...c,
        contact_id: c.id,
        id: `${c.id}:${c.updated_at}`,
      }));
    },
    sample: { ...CONTACT_SAMPLE, contact_id: CONTACT_SAMPLE.id },
    outputFields: [{ key: 'contact_id', label: 'Contact ID' }, ...CONTACT_OUTPUT],
  },
};
