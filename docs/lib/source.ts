import { docs } from 'fumadocs-mdx:collections/server';
import { type InferPageType, loader } from 'fumadocs-core/source';
import { lucideIconsPlugin } from 'fumadocs-core/source/lucide-icons';

// See https://fumadocs.dev/docs/headless/source-api for more info
export const source = loader({
  baseUrl: '/',
  source: docs.toFumadocsSource(),
  plugins: [lucideIconsPlugin()],
});

export function getPageImage(page: InferPageType<typeof source>) {
  const segments = [...page.slugs, 'image.png'];

  return {
    segments,
    url: `/og/docs/${segments.join('/')}`,
  };
}

// Raw-Markdown mirror of a page; the `.mdx` last segment makes the export emit real files.
export function getPageMarkdown(page: InferPageType<typeof source>) {
  const slugs = page.slugs.length > 0 ? page.slugs : ['index'];
  const segments = [...slugs.slice(0, -1), `${slugs.at(-1)}.mdx`];

  return {
    segments,
    url: `/llms.mdx/docs/${segments.join('/')}`,
  };
}

export async function getLLMText(page: InferPageType<typeof source>) {
  const processed = await page.data.getText('processed');

  return `# ${page.data.title}

${processed}`;
}
