/**
 * Single source of truth for the Learn library.
 *
 * Order matters: it is the reading sequence, and drives the left-rail list,
 * the index rows, and the prev/next footer on every article. Keep this file
 * in sync with the actual pages under src/pages/learn/.
 */
export interface LearnArticle {
  href: string;
  /** Display title used in rails, lists and cards (not the SEO <title>). */
  title: string;
  topic: 'Foundations' | 'Sending well';
  level: 'Essential' | 'Beginner' | 'Practical' | 'Advanced';
  /** Reading time in minutes. */
  read: number;
  /** One-line summary shown on the library index. */
  body: string;
  /** Icon.astro name for cards. */
  icon: string;
  featured?: boolean;
}

export const learnArticles: LearnArticle[] = [
  {
    href: '/learn/deliverability/',
    title: 'The deliverability handbook',
    topic: 'Foundations',
    level: 'Essential',
    read: 14,
    body: 'A full mental model for getting cold mail into the inbox and keeping it there. Authentication, warmup, volume discipline and reputation, in one place.',
    icon: 'shield',
    featured: true,
  },
  {
    href: '/learn/email-warmup/',
    title: 'Email warmup, properly explained',
    topic: 'Foundations',
    level: 'Beginner',
    read: 11,
    body: 'Why warmup exists, how providers read it, and how to do it without faking signals.',
    icon: 'flame',
  },
  {
    href: '/learn/spf-dkim-dmarc/',
    title: 'SPF, DKIM and DMARC, end to end',
    topic: 'Foundations',
    level: 'Beginner',
    read: 12,
    body: 'The three authentication records and the alignment rules that actually matter for cold mail.',
    icon: 'key',
  },
  {
    href: '/learn/inbox-placement/',
    title: 'Inbox placement, not just delivery',
    topic: 'Foundations',
    level: 'Beginner',
    read: 8,
    body: 'Delivery is a 200 OK. Placement is whether the recipient actually sees the message.',
    icon: 'inbox',
  },
  {
    href: '/learn/cold-email-rules/',
    title: 'Cold email rules across regions',
    topic: 'Sending well',
    level: 'Practical',
    read: 10,
    body: 'CAN-SPAM, GDPR, CASL, PECR. What each says and how to comply without lawyering up.',
    icon: 'list',
  },
  {
    href: '/learn/warmup-pools/',
    title: 'Warmup pools: free, premium, dedicated',
    topic: 'Sending well',
    level: 'Practical',
    read: 7,
    body: 'How shared warmup pools work and why pool quality matters more than pool size.',
    icon: 'users',
  },
  {
    href: '/learn/reputation-recovery/',
    title: 'Recovering a burned mailbox',
    topic: 'Sending well',
    level: 'Advanced',
    read: 9,
    body: 'A step-by-step plan to bring a quarantined mailbox back to healthy.',
    icon: 'workflow',
  },
  {
    href: '/learn/personalization/',
    title: 'Personalization & conditionals',
    topic: 'Sending well',
    level: 'Practical',
    read: 9,
    body: 'Merge variables, custom fields, and if/else conditionals so one template reads naturally for every recipient.',
    icon: 'list',
  },
];

export const glossaryHref = '/learn/glossary/';

/** The articles around `href` in reading order, for "Previously / Up next". */
export function neighborsOf(href: string): { prev?: LearnArticle; next?: LearnArticle } {
  const i = learnArticles.findIndex((a) => a.href === href);
  if (i === -1) {
    // Pages outside the sequence (e.g. the glossary) suggest the start of it.
    return { next: learnArticles[0] };
  }
  return {
    prev: i > 0 ? learnArticles[i - 1] : undefined,
    next: i < learnArticles.length - 1 ? learnArticles[i + 1] : undefined,
  };
}

/** Chip/dot tones per level, shared by the index cards and article headers. */
export function levelTone(level: LearnArticle['level']): { dot: string; chip: string } {
  if (level === 'Essential') return { dot: 'bg-amber-500', chip: 'bg-amber-50 text-amber-700' };
  if (level === 'Beginner') return { dot: 'bg-emerald-500', chip: 'bg-emerald-50 text-emerald-700' };
  if (level === 'Practical') return { dot: 'bg-sky-500', chip: 'bg-sky-50 text-sky-700' };
  return { dot: 'bg-violet-500', chip: 'bg-violet-50 text-violet-700' };
}

const NUMBER_WORDS = [
  'Zero', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight', 'Nine', 'Ten',
  'Eleven', 'Twelve',
];

/** "Seven" for 7; falls back to digits past twelve. */
export function numberWord(n: number): string {
  return NUMBER_WORDS[n] ?? String(n);
}
