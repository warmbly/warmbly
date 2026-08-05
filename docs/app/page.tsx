import type { Metadata } from 'next';

const TARGET = '/guides/mailboxes/';

export const metadata: Metadata = {
  title: 'Warmbly Documentation',
  robots: { index: false },
};

// No docs landing page. The inline script navigates while the HTML is still
// parsing, so this page never paints; the meta refresh and the link are the
// no-JS fallbacks (both work on static hosts).
export default function Home() {
  return (
    <>
      <meta httpEquiv="refresh" content={`0;url=${TARGET}`} />
      <script dangerouslySetInnerHTML={{ __html: `location.replace(${JSON.stringify(TARGET)})` }} />
      <noscript>
        <main className="flex min-h-screen items-center justify-center text-sm text-fd-muted-foreground">
          <a href={TARGET} className="underline underline-offset-4">
            Continue to the Warmbly docs
          </a>
        </main>
      </noscript>
    </>
  );
}
