import type { Metadata } from 'next';
import { Redirect } from '@/components/shared/redirect';

const TARGET = '/guides/';

export const metadata: Metadata = {
  title: 'Warmbly Documentation',
  robots: { index: false },
};

// The docs start on Getting started. The meta refresh works on any static host
// without JS; the client redirect covers navigation inside the app.
export default function Home() {
  return (
    <>
      <meta httpEquiv="refresh" content={`0;url=${TARGET}`} />
      <Redirect to={TARGET} />
      <main className="flex min-h-screen items-center justify-center text-sm text-fd-muted-foreground">
        <a href={TARGET} className="underline underline-offset-4">
          Continue to the Warmbly docs
        </a>
      </main>
    </>
  );
}
