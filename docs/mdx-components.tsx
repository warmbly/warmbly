import defaultMdxComponents from 'fumadocs-ui/mdx';
import { Step, Steps } from 'fumadocs-ui/components/steps';
import { Tab, Tabs, TabsList } from 'fumadocs-ui/components/tabs';
import { BookOpen, Braces } from 'lucide-react';
import type { MDXComponents } from 'mdx/types';
import { LangTab } from '@/components/shared/lang-tabs';
import { Mermaid } from '@/components/shared/mermaid';
import { Fold } from '@/components/shared/fold';
import { ShortVersion } from '@/components/diagrams/frame';
import { DeliveryVsPlacement, MailJourney, PlacementScale, SignalWeights } from '@/components/diagrams/mail';
import { HealthBands, PoolTiers, RecoveryRamp, WarmupExchange, WarmupRamp } from '@/components/diagrams/warmup';
import { AuthChecks, ComplianceMap, DmarcRollout, MergePreview, SubdomainPattern } from '@/components/diagrams/auth';

export function getMDXComponents(components?: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    Step,
    Steps,
    Tab,
    Tabs,
    TabsList,
    LangTab,
    Mermaid,
    Fold,
    ShortVersion,
    MailJourney,
    DeliveryVsPlacement,
    PlacementScale,
    SignalWeights,
    WarmupRamp,
    WarmupExchange,
    HealthBands,
    PoolTiers,
    RecoveryRamp,
    AuthChecks,
    DmarcRollout,
    SubdomainPattern,
    ComplianceMap,
    MergePreview,
    BookOpen,
    Braces,
    ...components,
  };
}
