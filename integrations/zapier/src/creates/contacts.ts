import type { Bundle, ZObject } from '../types';
import { api, pruneEmpty } from '../lib/client';

const CONTACT_SAMPLE = {
  id: '7b6c1f2a-0000-0000-0000-000000000000',
  email: 'lead@example.com',
  first_name: 'Sam',
  last_name: 'Rivera',
  company: 'Example Co',
  phone: '+15551234567',
  subscribed: true,
  custom_fields: {},
  created_at: '2026-06-28T12:00:00Z',
  updated_at: '2026-06-28T12:00:00Z',
};

export const createContact = {
  key: 'createContact',
  noun: 'Contact',
  display: {
    label: 'Create or Update Contact',
    description:
      'Adds a contact (or updates the existing one with the same email) and optionally enrolls it into campaigns. Custom fields merge by key.',
  },
  operation: {
    inputFields: [
      { key: 'email', label: 'Email', type: 'string', required: true },
      { key: 'first_name', label: 'First name', type: 'string' },
      { key: 'last_name', label: 'Last name', type: 'string' },
      { key: 'company', label: 'Company', type: 'string' },
      { key: 'phone', label: 'Phone', type: 'string' },
      {
        key: 'campaigns',
        label: 'Enroll in campaigns',
        type: 'string',
        list: true,
        dynamic: 'campaignList.id.name',
        helpText: 'Campaigns to enroll this contact into as a lead.',
      },
      {
        key: 'categories',
        label: 'Category IDs',
        type: 'string',
        list: true,
        helpText: 'Contact category UUIDs to assign.',
      },
      {
        key: 'custom_fields',
        label: 'Custom fields',
        dict: true,
        helpText: 'Free-form key/value attributes stored on the contact.',
      },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const contact = pruneEmpty({
        email: bundle.inputData.email,
        first_name: bundle.inputData.first_name,
        last_name: bundle.inputData.last_name,
        company: bundle.inputData.company,
        phone: bundle.inputData.phone,
        campaigns: bundle.inputData.campaigns,
        categories: bundle.inputData.categories,
        custom_fields: bundle.inputData.custom_fields,
      });
      // POST /v1/contacts takes a JSON array and returns an array.
      const response = await z.request({
        url: api('/contacts'),
        method: 'POST',
        body: [contact],
      });
      const created = Array.isArray(response.data) ? response.data[0] : response.data;
      return created;
    },
    sample: CONTACT_SAMPLE,
  },
};

export const updateContact = {
  key: 'updateContact',
  noun: 'Contact',
  display: {
    label: 'Update Contact',
    description: 'Updates an existing contact by ID. Email cannot be changed.',
  },
  operation: {
    inputFields: [
      { key: 'contact_id', label: 'Contact ID', type: 'string', required: true },
      { key: 'first_name', label: 'First name', type: 'string' },
      { key: 'last_name', label: 'Last name', type: 'string' },
      { key: 'company', label: 'Company', type: 'string' },
      { key: 'phone', label: 'Phone', type: 'string' },
      { key: 'subscribed', label: 'Subscribed', type: 'boolean' },
      {
        key: 'campaigns',
        label: 'Set campaigns',
        type: 'string',
        list: true,
        dynamic: 'campaignList.id.name',
        helpText: 'Replaces the full set of campaigns this contact belongs to.',
      },
      { key: 'custom_fields', label: 'Custom fields', dict: true },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const body = pruneEmpty({
        first_name: bundle.inputData.first_name,
        last_name: bundle.inputData.last_name,
        company: bundle.inputData.company,
        phone: bundle.inputData.phone,
        subscribed: bundle.inputData.subscribed,
        campaigns: bundle.inputData.campaigns,
        custom_fields: bundle.inputData.custom_fields,
      });
      const response = await z.request({
        url: api(`/contacts/${bundle.inputData.contact_id}`),
        method: 'PATCH',
        body,
      });
      return response.data;
    },
    sample: CONTACT_SAMPLE,
  },
};

export const addContactToCampaign = {
  key: 'addContactToCampaign',
  noun: 'Contact',
  display: {
    label: 'Add Contact to Campaign',
    description:
      'Adds a contact (creating it if new) as a lead in a campaign. Idempotent by email.',
  },
  operation: {
    inputFields: [
      { key: 'email', label: 'Email', type: 'string', required: true },
      {
        key: 'campaign_id',
        label: 'Campaign',
        type: 'string',
        required: true,
        dynamic: 'campaignList.id.name',
      },
      { key: 'first_name', label: 'First name', type: 'string' },
      { key: 'last_name', label: 'Last name', type: 'string' },
      { key: 'company', label: 'Company', type: 'string' },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const contact = pruneEmpty({
        email: bundle.inputData.email,
        first_name: bundle.inputData.first_name,
        last_name: bundle.inputData.last_name,
        company: bundle.inputData.company,
        campaigns: [bundle.inputData.campaign_id],
      });
      const response = await z.request({
        url: api('/contacts'),
        method: 'POST',
        body: [contact],
      });
      const created = Array.isArray(response.data) ? response.data[0] : response.data;
      return created;
    },
    sample: CONTACT_SAMPLE,
  },
};

export const createContactNote = {
  key: 'createContactNote',
  noun: 'Note',
  display: {
    label: 'Create Contact Note',
    description: 'Adds a note to a contact.',
  },
  operation: {
    inputFields: [
      { key: 'contact_id', label: 'Contact ID', type: 'string', required: true },
      { key: 'content', label: 'Note', type: 'text', required: true },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const response = await z.request({
        url: api(`/contacts/${bundle.inputData.contact_id}/notes`),
        method: 'POST',
        body: { content: bundle.inputData.content },
      });
      return response.data;
    },
    sample: {
      id: 'no1a2b3c-0000-0000-0000-000000000000',
      contact_id: '7b6c1f2a-0000-0000-0000-000000000000',
      content: 'Met at the conference.',
      created_at: '2026-06-28T12:00:00Z',
    },
  },
};
