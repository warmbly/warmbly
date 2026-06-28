import type { Bundle, ZObject } from '../types';
import { api, pruneEmpty } from '../lib/client';

const CAMPAIGN_SAMPLE = {
  id: 'ca1b2c3d-0000-0000-0000-000000000000',
  name: 'Q3 Outbound',
  description: '',
  status: 'draft',
  daily_limit: 50,
  created_at: '2026-06-28T12:00:00Z',
};

export const createCampaign = {
  key: 'createCampaign',
  noun: 'Campaign',
  display: {
    label: 'Create Campaign',
    description:
      'Creates a draft campaign. Add sequence steps, senders, and leads in Warmbly, then start it.',
  },
  operation: {
    inputFields: [
      { key: 'name', label: 'Name', type: 'string', required: true },
      { key: 'description', label: 'Description', type: 'text' },
      {
        key: 'daily_limit',
        label: 'Daily send cap',
        type: 'integer',
        helpText: 'Per-mailbox cold-send cap. Defaults to 50.',
      },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const body = pruneEmpty({
        name: bundle.inputData.name,
        description: bundle.inputData.description,
        daily_limit: bundle.inputData.daily_limit,
      });
      const response = await z.request({
        url: api('/campaigns'),
        method: 'POST',
        body,
      });
      return response.data;
    },
    sample: CAMPAIGN_SAMPLE,
  },
};

const lifecycle = (action: 'start' | 'stop') =>
  async (z: ZObject, bundle: Bundle) => {
    const response = await z.request({
      url: api(`/campaigns/${bundle.inputData.campaign_id}/${action}`),
      method: 'POST',
    });
    return { campaign_id: bundle.inputData.campaign_id, ...response.data };
  };

export const startCampaign = {
  key: 'startCampaign',
  noun: 'Campaign',
  display: {
    label: 'Start Campaign',
    description: 'Starts (or resumes) a campaign so it begins sending.',
  },
  operation: {
    inputFields: [
      {
        key: 'campaign_id',
        label: 'Campaign',
        type: 'string',
        required: true,
        dynamic: 'campaignList.id.name',
      },
    ],
    perform: lifecycle('start'),
    sample: { campaign_id: CAMPAIGN_SAMPLE.id, status: 'started' },
  },
};

export const stopCampaign = {
  key: 'stopCampaign',
  noun: 'Campaign',
  display: {
    label: 'Stop Campaign',
    description: 'Pauses an active campaign.',
  },
  operation: {
    inputFields: [
      {
        key: 'campaign_id',
        label: 'Campaign',
        type: 'string',
        required: true,
        dynamic: 'campaignList.id.name',
      },
    ],
    perform: lifecycle('stop'),
    sample: { campaign_id: CAMPAIGN_SAMPLE.id, status: 'stopped' },
  },
};
