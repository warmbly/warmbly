import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

// The Warmbly mark, same single path as site/src/components/Logo.astro, rendered
// monochrome via currentColor so it is black in light mode and white in dark
// mode automatically (no color version, no invert filter).
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
          <span className="font-semibold">Warmbly</span>
        </div>
      ),
    },
    links: [
      {
        text: 'Dashboard',
        url: 'https://app.warmbly.com',
      },
    ],
  };
}
