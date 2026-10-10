'use client';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useState } from 'react';
import { BookOpen, Check, ChevronsUpDown, Code2, GraduationCap, Server } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from 'fumadocs-ui/components/ui/popover';
import { cn } from '@/lib/cn';

// The four doc sections; the header switcher moves between them from any page.
const SECTIONS = [
  { prefix: '/guides', href: '/guides/', name: 'Guides', text: 'Using the product, step by step', icon: BookOpen },
  { prefix: '/learn', href: '/learn/email-warmup/', name: 'Learn', text: 'Cold email and deliverability basics', icon: GraduationCap },
  { prefix: '/api', href: '/api/', name: 'API reference', text: 'REST API, SDKs, webhooks, CLI and MCP', icon: Code2 },
  { prefix: '/development', href: '/development/install/', name: 'Self-hosting', text: 'Install and run your own instance', icon: Server },
];

export function SectionSwitcher() {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const current = SECTIONS.find((s) => pathname.startsWith(s.prefix)) ?? SECTIONS[0];
  const Icon = current.icon;

  return (
    <div className="flex items-center">
      <span aria-hidden="true" className="mx-3 h-5 w-px rotate-12 bg-fd-border" />
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger className="inline-flex h-8 items-center gap-2 rounded-md px-2 text-[14px] font-medium text-fd-foreground transition-colors hover:bg-fd-accent">
          <Icon className="size-4 text-fd-muted-foreground" />
          {current.name}
          <ChevronsUpDown className="size-3.5 text-fd-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-1.5">
          {SECTIONS.map((s) => {
            const active = s === current;
            const ItemIcon = s.icon;
            return (
              <Link
                key={s.prefix}
                href={s.href}
                onClick={() => setOpen(false)}
                className={cn(
                  'flex items-start gap-3 rounded-md px-2.5 py-2 transition-colors hover:bg-fd-accent',
                  active && 'bg-fd-accent',
                )}
              >
                <span className="mt-0.5 inline-flex size-7 shrink-0 items-center justify-center rounded-md border border-fd-border bg-fd-background text-fd-foreground">
                  <ItemIcon className="size-3.5" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-[13.5px] font-medium text-fd-foreground">{s.name}</span>
                  <span className="block text-[12.5px] text-fd-muted-foreground">{s.text}</span>
                </span>
                {active && <Check className="mt-1 size-3.5 text-fd-foreground" />}
              </Link>
            );
          })}
        </PopoverContent>
      </Popover>
    </div>
  );
}
