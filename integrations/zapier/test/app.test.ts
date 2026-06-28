import App from '../src/index';
import { pruneEmpty, listData } from '../src/lib/client';

const schema = require('zapier-platform-schema');

// Replace live functions with the runtime placeholder so the schema validates
// the rest of the definition (this is what `zapier build` does before upload).
const placeholder = (v: any): any => {
  if (typeof v === 'function') return '$func$0$f$';
  if (Array.isArray(v)) return v.map(placeholder);
  if (v && typeof v === 'object') {
    const o: any = {};
    for (const k of Object.keys(v)) o[k] = placeholder(v[k]);
    return o;
  }
  return v;
};

describe('Warmbly Zapier app', () => {
  it('passes Zapier schema validation', () => {
    const results = schema.validateAppDefinition(placeholder(App));
    expect(results.errors).toHaveLength(0);
  });

  it('uses OAuth2 with a /me connection label', () => {
    expect((App.authentication as any).type).toBe('oauth2');
    expect((App.authentication as any).connectionLabel).toContain('email');
  });

  it('exposes the expected triggers, creates, and searches', () => {
    expect(Object.keys(App.triggers)).toEqual(
      expect.arrayContaining([
        'newContact',
        'newOrUpdatedContact',
        'newDeal',
        'dealWon',
        'newCrmTask',
        'newMeeting',
        'newInboundEmail',
        'newCampaign',
        'campaignCompleted',
        'newMailbox',
      ]),
    );
    expect(Object.keys(App.creates)).toEqual(
      expect.arrayContaining([
        'createContact',
        'updateContact',
        'addContactToCampaign',
        'sendEmail',
        'replyToEmail',
        'createDeal',
        'startCampaign',
      ]),
    );
    expect(Object.keys(App.searches)).toEqual(
      expect.arrayContaining(['findContact', 'findCampaign', 'findMailbox']),
    );
  });

  it('hides the dropdown list triggers but not user-facing ones', () => {
    expect((App.triggers as any).campaignList.display.hidden).toBe(true);
    expect((App.triggers as any).newContact.display.hidden).toBeFalsy();
  });
});

describe('helpers', () => {
  it('pruneEmpty removes empty values but keeps 0 / false / empty arrays', () => {
    expect(
      pruneEmpty({ a: '', b: undefined, c: null, d: 0, e: false, f: 'x', g: [] }),
    ).toEqual({ d: 0, e: false, f: 'x', g: [] });
  });

  it('listData unwraps a {data:[...]} envelope and bare arrays', () => {
    expect(listData({ data: { data: [1, 2] } })).toEqual([1, 2]);
    expect(listData({ data: [3, 4] })).toEqual([3, 4]);
    expect(listData({ data: null })).toEqual([]);
  });
});
