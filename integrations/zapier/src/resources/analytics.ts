import type { Bundle, ZObject } from '../lib/types';
import { api, pruneEmpty } from '../lib/client';

const getCampaignAnalytics = {
  key: 'getCampaignAnalytics',
  noun: 'Analytics',
  display: {
    label: 'Get Campaign Analytics',
    description: 'Fetches summary stats (sends, opens, clicks, replies, bounces) for a campaign.',
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
      {
        key: 'from',
        label: 'From',
        type: 'string',
        helpText:
          'First day of the period, YYYY-MM-DD (UTC). Fill in both From and To, or leave both empty for all time. The period counts the emails sent on those days, with every open, click, reply and bounce they earned.',
      },
      {
        key: 'to',
        label: 'To',
        type: 'string',
        helpText: 'Last day of the period, YYYY-MM-DD (UTC), included.',
      },
    ],
    perform: async (z: ZObject, bundle: Bundle): Promise<any[]> => {
      const response = await z.request({
        url: api(`/analytics/campaigns/${bundle.inputData.campaign_id}`),
        params: pruneEmpty({ from: bundle.inputData.from, to: bundle.inputData.to }),
      });
      return response.data ? [response.data] : [];
    },
    sample: {
      campaign_id: 'ca1b2c3d-0000-0000-0000-000000000000',
      name: 'Q3 Outbound',
      status: 'active',
      summary: {
        emails_sent: 420,
        unique_opens: 210,
        unique_clicks: 38,
        replies: 12,
        bounces: 3,
        open_rate: 50,
        reply_rate: 2.9,
        positive_replies: 4,
        positive_reply_rate: 0.9,
        interested_leads: 4,
      },
    },
  },
};

const getDashboardAnalytics = {
  key: 'getDashboardAnalytics',
  noun: 'Analytics',
  display: {
    label: 'Get Dashboard Analytics',
    description: 'Fetches workspace-wide stats for a period (7d, 30d, or 90d).',
  },
  operation: {
    inputFields: [
      {
        key: 'period',
        label: 'Period',
        type: 'string',
        choices: { '7d': 'Last 7 days', '30d': 'Last 30 days', '90d': 'Last 90 days' },
        default: '7d',
      },
    ],
    perform: async (z: ZObject, bundle: Bundle): Promise<any[]> => {
      const response = await z.request({
        url: api('/analytics/dashboard'),
        params: { period: bundle.inputData.period || '7d' },
      });
      return response.data ? [response.data] : [];
    },
    sample: {
      period: '7d',
      overall_stats: {
        total_emails_sent: 1280,
        total_opens: 640,
        total_replies: 44,
        open_rate: 50,
        reply_rate: 3.4,
        active_campaigns: 6,
        active_accounts: 9,
      },
    },
  },
};

export const triggers: Array<{ key: string }> = [];
export const creates: Array<{ key: string }> = [];
export const searches = [getCampaignAnalytics, getDashboardAnalytics];
