'use client';
import type { ReactNode } from 'react';
import { RootProvider } from 'fumadocs-ui/provider/next';

// next-themes renders its no-flash script inside a component. The server copy runs
// on load; on the client it is marked a data block so React has nothing to warn about.
const scriptProps = { type: typeof window === 'undefined' ? 'text/javascript' : 'text/plain' };

export function Provider({ children }: { children: ReactNode }) {
  return (
    // `type: 'static'` pairs with the staticGET search route so search runs
    // client-side from a prebuilt index (works on a static build).
    <RootProvider search={{ options: { type: 'static' } }} theme={{ defaultTheme: 'light', scriptProps }}>
      {children}
    </RootProvider>
  );
}
