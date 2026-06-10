import Link from 'next/link';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Warmbly Documentation',
  robots: { index: false },
};

// The docs root has no landing page of its own: it sends readers straight to
// the Guides section. A meta refresh keeps this working on a fully static
// host, where server-side redirects are not available.
export default function Home() {
  return (
    <>
      <meta httpEquiv="refresh" content="0;url=/guides/mailboxes/" />
      <main className="flex min-h-screen items-center justify-center text-sm text-fd-muted-foreground">
        <Link href="/guides/mailboxes/" className="underline underline-offset-4">
          Continue to the Warmbly docs
        </Link>
      </main>
    </>
  );
}
