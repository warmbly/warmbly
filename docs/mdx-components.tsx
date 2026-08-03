import defaultMdxComponents from 'fumadocs-ui/mdx';
import { Tab, Tabs, TabsList } from 'fumadocs-ui/components/tabs';
import { BookOpen, Braces } from 'lucide-react';
import type { MDXComponents } from 'mdx/types';
import { LangTab } from '@/components/shared/lang-tabs';
import { Mermaid } from '@/components/shared/mermaid';

export function getMDXComponents(components?: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    Tab,
    Tabs,
    TabsList,
    LangTab,
    Mermaid,
    BookOpen,
    Braces,
    ...components,
  };
}
