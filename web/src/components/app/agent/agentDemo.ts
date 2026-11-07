// Dev-only scripted agent run. AgentPanel plays these through the same event
// handler a real SSE stream feeds, so the whole run UI (status line, tool
// trace, streamed text, approval card) can be seen without an AI provider.
// Loaded through a dynamic import behind import.meta.env.DEV, so production
// builds never include it. Start one with "/demo" in the composer.

import type { AgentStreamEvent } from "@/lib/api/models/app/agent/Agent";

export type DemoStep = { wait: number; ev: AgentStreamEvent };

export const DEMO_PROMPT =
    "How are my campaigns doing? Pause anything that is hurting deliverability.";

const CAMPAIGN = 'campaign: "Agency re-engagement"';

// Streams a reply the way a provider does: small word groups at an uneven
// pace, then the authoritative full block.
function say(text: string): DemoStep[] {
    const out: DemoStep[] = [];
    const words = text.split(/(\s+)/);
    for (let i = 0; i < words.length; i += 4) {
        out.push({
            wait: 30 + ((i * 7) % 50),
            ev: { type: "text_delta", text: words.slice(i, i + 4).join("") },
        });
    }
    out.push({ wait: 120, ev: { type: "text", text } });
    return out;
}

function tool(name: string, args: string, result: string, ms: number): DemoStep[] {
    return [
        { wait: 260, ev: { type: "tool_start", tool: name, args_summary: args } },
        { wait: ms, ev: { type: "tool_result", tool: name, result } },
    ];
}

function iteration(n: number, balanceUsd: number): DemoStep {
    return {
        wait: 200,
        ev: { type: "iteration", iteration: n, budget: 12, balance_usd: balanceUsd },
    };
}

export function demoRun(part: "open" | "approve" | "deny"): DemoStep[] {
    if (part === "approve") {
        return [
            iteration(4, 4.79),
            ...tool("set_campaign_status", `${CAMPAIGN}, status: "paused"`, "Paused Agency re-engagement", 1100),
            { wait: 700, ev: { type: "iteration", iteration: 5, budget: 12 } },
            ...say(
                "Done. **Agency re-engagement** is paused and nothing else was changed. Resume it from the campaign page once the list is cleaned.",
            ),
            { wait: 100, ev: { type: "done", balance_usd: 4.78 } },
        ];
    }
    if (part === "deny") {
        return [
            iteration(4, 4.79),
            ...say(
                "Okay, I left it running. Keep an eye on its bounce rate: at 10% the sending mailbox can be blocked from the warmup pool.",
            ),
            { wait: 100, ev: { type: "done", balance_usd: 4.79 } },
        ];
    }
    return [
        { wait: 900, ev: { type: "iteration", iteration: 1, budget: 12, balance_usd: 4.82 } },
        ...tool("list_campaigns", "status: active", "4 active campaigns", 1100),
        iteration(2, 4.81),
        ...tool("get_campaign_stats", 'campaign: "Q4 founder outreach"', "41% opened, 6.2% replied, 0.4% bounced", 1200),
        ...tool("get_campaign_stats", CAMPAIGN, "9% opened, 0.3% replied, 7.8% bounced", 1000),
        ...tool("get_mailbox", 'mailbox: "sam@getacme.io"', "warmup throttled, 22% spam placement", 900),
        ...tool("get_advisor_summary", "", "2 open recommendations", 800),
        iteration(3, 4.80),
        { wait: 1200, ev: { type: "iteration", iteration: 3, budget: 12 } },
        ...say(
            [
                "Three of your four active campaigns look healthy. **Q4 founder outreach** is the standout at 41% opened and 6.2% replied.",
                "",
                "**Agency re-engagement** is the problem:",
                "",
                "- 7.8% of its sends bounced this week, past the 5% line where providers start to push back",
                "- it sends from `sam@getacme.io`, which is already throttled at 22% spam placement",
                "- only 0.3% replied, so pausing it costs almost nothing",
                "",
                "I'd pause it now and clean the list before it runs again.",
            ].join("\n"),
        ),
        {
            wait: 500,
            ev: {
                type: "approval_required",
                tool: "set_campaign_status",
                risk: "write",
                tool_call_id: "demo-1",
                args_summary: `${CAMPAIGN}, status: "paused"`,
                arguments: JSON.stringify({ action: "stop", campaign_id: CAMPAIGN }, null, 2),
            },
        },
    ];
}
