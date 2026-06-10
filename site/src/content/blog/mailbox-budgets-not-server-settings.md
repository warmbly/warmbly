---
title: 'Your send limit is a mailbox budget, not a server setting'
description: 'People keep asking us to raise their daily limit. The honest answer is that the limit they are asking about does not exist.'
pubDate: 2026-06-10
tags: ['sending', 'deliverability']
---

A support question we get a lot, in various forms: "can you raise my sending limit to 500 a day?"

Sure. Technically that takes us about thirty seconds. The scheduler reads a number from a column, we change the number. But the question assumes the limit is ours, like an API quota we could lift for paying customers. It isn't. The real limit is set by Gmail and Outlook, per mailbox, based on how that mailbox has behaved. We just default to a number that keeps you under it.

That default is 50 campaign emails per mailbox per day, with a minimum of 600 seconds between sends from the same mailbox. You can push a mailbox to 100 in settings, and the validation will stop you there, but the default is 50 because that is roughly where a normal, healthy mailbox with a few months of history stops looking suspicious.

The 600-second gap matters more than people think. Mailbox providers see timing. A human with a busy morning sends 12 emails between 9:00 and 11:30 with messy spacing. A script sends 40 emails at 9:00:00, 9:00:01, 9:00:02. You can have a pristine domain and still get foldered for that pattern alone.

## So how do you actually send more?

More mailboxes. That's the whole answer.

If you need 1,000 sends a day, that is twenty mailboxes at the default cap, ideally spread over a few domains. It is genuinely more setup work than typing 1000 into a box, and it is also the only version that survives contact with Gmail.

This shapes our infrastructure too, in a way that took us a while to get right. Workers (the processes that actually deliver mail) do not have their own send limits. A worker's capacity is the sum of the budgets of the mailboxes assigned to it, nothing more. Early on we were tempted to give workers a global throttle as a safety net, and we ended up removing the idea: it either duplicates the per-mailbox math or silently overrides it, and both are bugs. What we kept instead is an assignment rule. We don't pile many active sending mailboxes onto one worker, because every worker is also an IP address, and concentrating traffic through one IP recreates the exact problem the mailbox budgets were preventing.

One more thing about raising the cap, since the settings page will let you go to 100. Before you do, look at three numbers for that mailbox: complaint rate, hard bounce rate, and where your warmup messages have been landing. If all three have been quiet for a few weeks, going from 50 to 60 or 70 is reasonable. If you don't know what those numbers are, that is itself the answer.
