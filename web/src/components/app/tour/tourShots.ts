// The real dashboard, as the guides show it: the "Sunrise Labs" sample
// workspace. Copied from docs/public/guides; refresh them together.

import accounts from "./shots/accounts.webp";
import addAccount from "./shots/add-account.webp";
import analytics from "./shots/analytics.webp";
import automationBuilder from "./shots/automation-builder.webp";
import automations from "./shots/automations.webp";
import campaignOverview from "./shots/campaign-overview.webp";
import campaignSteps from "./shots/campaign-steps.webp";
import campaigns from "./shots/campaigns.webp";
import contacts from "./shots/contacts.webp";
import deals from "./shots/deals.webp";
import deliverability from "./shots/deliverability.webp";
import mailboxWarmup from "./shots/mailbox-warmup.webp";
import sendingDomains from "./shots/sending-domains.webp";
import unibox from "./shots/unibox.webp";
import avatar from "./shots/warmbly-avatar.webp";

export {
    accounts,
    addAccount,
    analytics,
    automationBuilder,
    automations,
    avatar,
    campaignOverview,
    campaignSteps,
    campaigns,
    contacts,
    deals,
    deliverability,
    mailboxWarmup,
    sendingDomains,
    unibox,
};

/** Every image a scene shows, so the tour can fetch them before they are needed. */
export const SHOTS = [
    accounts,
    addAccount,
    mailboxWarmup,
    sendingDomains,
    campaigns,
    campaignSteps,
    campaignOverview,
    contacts,
    unibox,
    analytics,
    deliverability,
    deals,
    automations,
    automationBuilder,
    avatar,
];
