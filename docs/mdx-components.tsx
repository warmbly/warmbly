import defaultMdxComponents from 'fumadocs-ui/mdx';
import { Tab, Tabs, TabsList } from 'fumadocs-ui/components/tabs';
import { BookOpen, Braces } from 'lucide-react';
import type { MDXComponents } from 'mdx/types';
import { LangTab } from '@/components/shared/lang-tabs';

export function getMDXComponents(components?: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    Tab,
    Tabs,
    TabsList,
    LangTab,
    BookOpen,
    Braces,
    ...components,
  };
}
