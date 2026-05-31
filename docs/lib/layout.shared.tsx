import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import Image from 'next/image';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <div className="flex items-center gap-2">
          <Image
            src="/logo.svg"
            alt="Warmbly"
            width={24}
            height={25}
            className="dark:invert"
          />
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
