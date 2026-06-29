import type { Bundle, ZObject } from './types';
import { api, listData } from './client';

type PollOptions = {
  // Extra query params, possibly derived from the user's trigger input.
  query?: (bundle: Bundle) => Record<string, any>;
  // Per-item transform (e.g. override `id` so updates re-trigger, drop noise).
  map?: (item: any, bundle: Bundle) => any;
  // Client-side filter (for facets the API can't express, e.g. status).
  filter?: (item: any, bundle: Bundle) => boolean;
};

// Build a polling trigger `perform` for a GET list endpoint that returns a
// `{ data: [...] }` envelope. Zapier dedupes the returned array by each item's
// `id`, so the endpoint only needs to surface recent rows (limit 100) — exact
// ordering is not required for correctness.
export const pollList =
  (path: string, opts: PollOptions = {}) =>
  async (z: ZObject, bundle: Bundle): Promise<any[]> => {
    const response = await z.request({
      url: api(path),
      params: { limit: 100, ...(opts.query ? opts.query(bundle) : {}) },
    });
    let items = listData(response);
    if (opts.filter) {
      items = items.filter((item) => opts.filter!(item, bundle));
    }
    if (opts.map) {
      items = items.map((item) => opts.map!(item, bundle));
    }
    return items;
  };
