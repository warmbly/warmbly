import defaultMdxComponents from 'fumadocs-ui/mdx';
import { Tab, Tabs } from 'fumadocs-ui/components/tabs';
import { BookOpen, Braces } from 'lucide-react';
import type { MDXComponents } from 'mdx/types';

export function getMDXComponents(components?: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    Tab,
    Tabs,
    BookOpen,
    Braces,
    ...components,
  };
}
