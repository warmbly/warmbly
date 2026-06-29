import type { Bundle, ZObject } from 'zapier-platform-core';

export type { Bundle, ZObject };

// A trigger/create/search perform. Loosely typed return because Zapier maps
// the value into its own runtime envelope.
export type Perform<T = unknown> = (z: ZObject, bundle: Bundle) => Promise<T>;

// Shape every resource module exports so index.ts can assemble the app.
export interface ResourceModule {
  triggers?: Array<{ key: string }>;
  creates?: Array<{ key: string }>;
  searches?: Array<{ key: string }>;
}
