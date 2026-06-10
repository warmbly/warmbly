---
title: "What Google's bulk sender rules actually ask of cold emailers"
description: 'We re-read the requirements so you do not have to. Most of it is things you should have been doing anyway, but two details catch people off guard.'
pubDate: 2026-06-08
tags: ['deliverability']
---

Every few months the bulk sender requirements make the rounds again and someone asks us whether cold email is "dead now". We have read these documents more times than we would like. Here is what they actually say, and the two places where senders genuinely get caught out.

The headline requirements: SPF and DKIM on your sending domain, a DMARC policy published, alignment between the From domain and what got authenticated, one-click unsubscribe on bulk mail, and a user-reported spam rate that stays under 0.10% and never touches 0.30%.

Most of that list is hygiene that predates the rules. The two that bite:

**Alignment, not just authentication.** This is the one we see most often in support. A domain can pass SPF and DKIM and still fail DMARC, because the passing signature belongs to a sending service's domain instead of yours. Dashboards love showing green checkmarks for "SPF: pass, DKIM: pass" while alignment is quietly broken. The only check we trust is opening the headers of a real delivered message and reading the `Authentication-Results` line yourself. If `dkim=pass` is followed by a `header.d=` that is not your domain, you have work to do.

**The spam rate math at small volume.** 0.10% sounds like a generous allowance until you do the division. A mailbox sending 50 cold emails a day sends about 1,500 a month. Staying under 0.10% means at most one spam complaint per month per mailbox. One. There is no clever trick that gets you under that threshold; the message has to be relevant enough that essentially nobody who receives it is angry about receiving it. Every other deliverability technique is downstream of that.

On unsubscribe: yes, the one-click requirement (RFC 8058, the List-Unsubscribe headers) is written for marketing mail to subscribers, and you can argue about whether a two-line intro email is "bulk". We would skip the argument. The recipient who wants out has exactly two buttons available, and one of them reports you to Google. Giving them the other one is not compliance, it is self-interest. Warmbly adds the headers and treats any opt-out as workspace-wide suppression, not a per-campaign flag.

Worth reading alongside this: Microsoft's guidance for Exchange Online, which says more or less openly that bulk commercial mail does not belong on their platform and should go through specialized providers. People read that as hostility. We read it as a description of the line both providers are drawing: traffic that looks like a person corresponding survives, traffic that looks like infrastructure gets pushed out. The bulk sender rules are that same line, written down with numbers attached.
