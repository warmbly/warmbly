import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';
import { BookOpen, Braces, GraduationCap } from 'lucide-react';
import type { Metadata } from 'next';

export const metadata: Metadata = {
  title: 'Page not found',
  robots: { index: false },
};

const sections = [
  {
    href: '/guides/mailboxes/',
    icon: BookOpen,
    title: 'Guides',
    description: 'How every part of the product works.',
  },
  {
    href: '/learn/email-warmup/',
    icon: GraduationCap,
    title: 'Learn',
    description: 'Cold email and deliverability fundamentals.',
  },
  {
    href: '/api/',
    icon: Braces,
    title: 'API Reference',
    description: 'Authenticate and drive Warmbly programmatically.',
  },
];

export default function NotFound() {
  return (
    <HomeLayout {...baseOptions()}>
      <main className="flex flex-1 flex-col items-center justify-center px-4 py-24 text-center">
        <p className="font-mono text-[11px] uppercase tracking-[0.22em] text-fd-muted-foreground">
          404
        </p>
        <h1 className="mt-3 text-3xl font-semibold tracking-tight text-fd-foreground">
          Page not found
        </h1>
        <p className="mt-3 max-w-md text-[15px] text-fd-muted-foreground">
          The page you are looking for does not exist or has moved. Try the
          search (press <kbd className="rounded-md border border-fd-border bg-fd-muted px-1.5 py-0.5 font-mono text-xs">/</kbd>)
          or jump back into a section.
        </p>
        <div className="mt-10 grid w-full max-w-2xl gap-3 sm:grid-cols-3">
          {sections.map((section) => (
            <Link
              key={section.href}
              href={section.href}
              className="group rounded-lg border border-fd-border bg-fd-card p-4 text-left transition-colors hover:border-fd-primary/40 hover:bg-fd-accent"
            >
              <section.icon className="size-4 text-fd-primary" />
              <p className="mt-2.5 text-sm font-medium text-fd-foreground">{section.title}</p>
              <p className="mt-1 text-[13px] leading-snug text-fd-muted-foreground">
                {section.description}
              </p>
            </Link>
          ))}
        </div>
      </main>
    </HomeLayout>
  );
}
