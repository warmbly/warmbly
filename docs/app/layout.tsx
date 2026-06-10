import { RootProvider } from 'fumadocs-ui/provider/next';
import './global.css';
import { Inter } from 'next/font/google';
import { KeyboardShortcutsProvider } from '@/components/shared/KeyboardShortcutsProvider';
import type { Metadata } from 'next';

const inter = Inter({
  subsets: ['latin'],
});

export const metadata: Metadata = {
  metadataBase: new URL('https://docs.warmbly.com'),
  title: {
    template: '%s | Warmbly Docs',
    default: 'Warmbly Documentation',
  },
  description: 'Guides and API reference for Warmbly: email warmup, cold outreach, and CRM.',
};

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={inter.className} suppressHydrationWarning>
      <body className="flex flex-col min-h-screen">
        {/* `type: 'static'` pairs with the staticGET search route so search runs
            client-side from a prebuilt index (works on a static build). */}
        <RootProvider search={{ options: { type: 'static' } }}>
          <KeyboardShortcutsProvider>
            {children}
          </KeyboardShortcutsProvider>
        </RootProvider>
      </body>
    </html>
  );
}
