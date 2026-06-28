import type { Bundle, ZObject } from '../types';
import { api, pruneEmpty } from '../lib/client';

const SEND_MODE_CHOICES = {
  instant: 'Instant',
  smart: 'Smart (respect the mailbox send gap)',
  scheduled: 'Scheduled (use the scheduled time)',
};

const SEND_RESULT_SAMPLE = {
  task_id: 'tk1a2b3c-0000-0000-0000-000000000000',
  scheduled_at: '2026-06-28T12:00:05Z',
  send_mode: 'instant',
};

const sendInputFields = (toHelp: string) => [
  {
    key: 'email_account_id',
    label: 'From mailbox',
    type: 'string',
    required: true,
    dynamic: 'mailboxList.id.email',
  },
  { key: 'to', label: 'To', type: 'string', list: true, required: true, helpText: toHelp },
  { key: 'subject', label: 'Subject', type: 'string', required: true },
  { key: 'body_html', label: 'HTML body', type: 'text' },
  { key: 'body_plain', label: 'Plain-text body', type: 'text' },
  { key: 'cc', label: 'CC', type: 'string', list: true },
  { key: 'bcc', label: 'BCC', type: 'string', list: true },
  {
    key: 'send_mode',
    label: 'Send mode',
    type: 'string',
    choices: SEND_MODE_CHOICES,
    default: 'instant',
  },
  {
    key: 'scheduled_at',
    label: 'Scheduled at',
    type: 'datetime',
    helpText: 'Only used when send mode is Scheduled. Must be in the future.',
  },
];

export const sendEmail = {
  key: 'sendEmail',
  noun: 'Email',
  display: {
    label: 'Send Email',
    description: 'Sends a one-off email from one of your connected mailboxes.',
  },
  operation: {
    inputFields: sendInputFields('Recipient email addresses.'),
    perform: async (z: ZObject, bundle: Bundle) => {
      const body = pruneEmpty({
        to: bundle.inputData.to,
        cc: bundle.inputData.cc,
        bcc: bundle.inputData.bcc,
        subject: bundle.inputData.subject,
        body_html: bundle.inputData.body_html,
        body_plain: bundle.inputData.body_plain,
        send_mode: bundle.inputData.send_mode,
        scheduled_at: bundle.inputData.scheduled_at,
      });
      const response = await z.request({
        url: api(`/emails/${bundle.inputData.email_account_id}/send`),
        method: 'POST',
        body,
      });
      return response.data;
    },
    sample: SEND_RESULT_SAMPLE,
  },
};

export const replyToEmail = {
  key: 'replyToEmail',
  noun: 'Email',
  display: {
    label: 'Reply in Inbox',
    description:
      'Sends a reply from a mailbox, optionally threading it into an existing conversation.',
  },
  operation: {
    inputFields: [
      ...sendInputFields('Recipient email addresses.'),
      {
        key: 'thread_id',
        label: 'Thread ID',
        type: 'string',
        helpText: 'Thread to attach the reply to (from a New Email Received trigger).',
      },
      {
        key: 'in_reply_to',
        label: 'In reply to (message IDs)',
        type: 'string',
        list: true,
      },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const body = pruneEmpty({
        email_account_id: bundle.inputData.email_account_id,
        to: bundle.inputData.to,
        cc: bundle.inputData.cc,
        bcc: bundle.inputData.bcc,
        subject: bundle.inputData.subject,
        body_html: bundle.inputData.body_html,
        body_plain: bundle.inputData.body_plain,
        thread_id: bundle.inputData.thread_id,
        in_reply_to: bundle.inputData.in_reply_to,
        send_mode: bundle.inputData.send_mode,
        scheduled_at: bundle.inputData.scheduled_at,
      });
      const response = await z.request({
        url: api('/unibox/reply'),
        method: 'POST',
        body,
      });
      return response.data;
    },
    sample: SEND_RESULT_SAMPLE,
  },
};

export const verifyEmail = {
  key: 'verifyEmail',
  noun: 'Verification',
  display: {
    label: 'Verify Email Address',
    description:
      'Runs a deliverability check (syntax, MX, SMTP probe, catch-all) on an address before you send.',
  },
  operation: {
    inputFields: [
      { key: 'email', label: 'Email', type: 'string', required: true },
    ],
    perform: async (z: ZObject, bundle: Bundle) => {
      const response = await z.request({
        url: api('/emails/verify'),
        method: 'POST',
        body: { email: bundle.inputData.email },
      });
      return response.data;
    },
    sample: {
      email: 'lead@example.com',
      status: 'valid',
      reason: 'deliverable',
      is_catch_all: false,
      has_mx: true,
      checked_at: '2026-06-28T12:00:00Z',
    },
  },
};
