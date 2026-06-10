import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

// The Warmbly mark (same path as site/'s Logo.astro), monochrome via currentColor.
function Mark() {
  return (
    <svg width="20" height="20" viewBox="0 0 746 764" fill="none" aria-hidden="true">
      <path
        d="M222.805 644.772L186.274 108.881L704.5 451.158L484.5 451.158L245.5 196.158L444 463.5L222.805 644.772Z"
        fill="currentColor"
      />
    </svg>
  );
}

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <div className="flex items-center gap-2">
          <Mark />
          <span className="font-extrabold tracking-tight text-[15px]">Warmbly</span>
          <span className="rounded-md border border-fd-border bg-fd-muted px-1.5 py-px text-[10px] font-medium uppercase tracking-[0.14em] text-fd-muted-foreground">
            Docs
          </span>
        </div>
      ),
    },
    githubUrl: 'https://github.com/warmbly/warmbly',
    links: [
      {
        text: 'warmbly.com',
        url: 'https://warmbly.com',
      },
      {
        text: 'Dashboard',
        url: 'https://app.warmbly.com',
      },
    ],
  };
}
