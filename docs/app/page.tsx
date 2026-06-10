import Link from 'next/link';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Warmbly Documentation',
  robots: { index: false },
};

// No docs landing page: a meta refresh (works on static hosts) sends readers to the guides.
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
