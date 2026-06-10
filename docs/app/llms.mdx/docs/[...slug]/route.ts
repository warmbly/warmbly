import { getLLMText, getPageMarkdown, source } from '@/lib/source';
import { notFound } from 'next/navigation';

export const revalidate = false;

// Every path ends in a real `<name>.mdx` file segment (the index page becomes
// `index.mdx`), so the static export writes plain files and a folder's own
// markdown (`/llms.mdx/docs/api.mdx`) never collides with the directory that
// holds its children (`/llms.mdx/docs/api/...`).
export async function GET(_req: Request, { params }: RouteContext<'/llms.mdx/docs/[...slug]'>) {
  const { slug } = await params;
  const last = slug.at(-1);
  if (!last?.endsWith('.mdx')) notFound();

  const slugs = [...slug.slice(0, -1), last.slice(0, -'.mdx'.length)];
  const page = source.getPage(slugs.length === 1 && slugs[0] === 'index' ? [] : slugs);
  if (!page) notFound();

  return new Response(await getLLMText(page), {
    headers: {
      'Content-Type': 'text/markdown',
    },
  });
}

export function generateStaticParams() {
  return source.getPages().map((page) => ({
    slug: getPageMarkdown(page).segments,
  }));
}
