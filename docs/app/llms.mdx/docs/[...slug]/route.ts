import { getLLMText, getPageMarkdown, source } from '@/lib/source';
import { notFound } from 'next/navigation';

export const revalidate = false;

// Paths end in a real `.mdx` filename so the export writes plain files with no
// file/directory collisions.
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
