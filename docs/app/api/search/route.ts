import { source } from '@/lib/source';
import { createFromSource } from 'fumadocs-core/search/server';

// Build the search index at build time and serve it as a static JSON, so search
// works on a statically-exported / CDN-hosted build (no running server route).
// The client is set to `type: 'static'` in app/layout.tsx, which loads this
// index once and runs Orama in the browser.
export const revalidate = false;

export const { staticGET: GET } = createFromSource(source, {
  // https://docs.orama.com/docs/orama-js/supported-languages
  language: 'english',
});
