// Result of POST /templates/score — an advisory content-quality score for a
// campaign template. Higher score = safer; issues are non-blocking hints to
// improve deliverability (the score never prevents saving or sending).

// Which half of the template a finding sits in.
export type TemplateField = "subject" | "body";

// One exact fragment that triggered an issue, so the editor can point at the
// words instead of restating the rule.
export interface TemplateScoreSpan {
    field: TemplateField;
    /** The fragment as it is written in the copy. */
    text: string;
    /** 1-based line within that field. */
    line?: number;
    /** The whole line, for context around the fragment. */
    excerpt?: string;
}

export interface TemplateScoreIssue {
    severity: "warn" | "high";
    code: string;
    message: string;
    /** Set when the issue lives in exactly one half of the template. */
    field?: TemplateField;
    spans?: TemplateScoreSpan[];
    suggestion?: string;
}

export default interface TemplateScore {
    score: number;
    issues: TemplateScoreIssue[];
}

// Body for POST /templates/score and POST /templates/analyze.
export interface ScoreTemplateRequest {
    subject: string;
    body_html: string;
    body_plain: string;
}

// One located problem the AI analysis found in the copy.
export interface SpamFinding {
    severity: "high" | "warn" | "info";
    /** Absent when the model labelled neither half and nothing in the finding
     *  could be anchored in the copy, so no badge is shown rather than one
     *  naming the wrong box. */
    field?: TemplateField;
    /** The exact fragment quoted from the copy, absent when the finding is
     *  about the email as a whole. Verified server-side against the template,
     *  so it is never a sentence the writer did not write. */
    text?: string;
    line?: number;
    excerpt?: string;
    issue: string;
    suggestion?: string;
    category?: string;
}

// How the copy reads to its recipient, judged by TypeSafe: calibrated numbers
// the panel thresholds on, not prose. reads_as and personalization are 0..1
// positions on their scale, 0 being the personal end.
export interface CopyJudgment {
    reads_as: number;
    personalization: number;
    ask: "one_clear_ask" | "several_asks" | "no_ask";
    /** Probability the copy makes a claim a spam filter would object to. */
    spam_claim: number;
    /** The lowest confidence across the scored answers. */
    confidence: number;
    model: string;
    input_tokens: number;
}

// Result of POST /templates/analyze — the AI half of the content check.
export interface TemplateAnalysis {
    score: number;
    verdict: string;
    findings: SpamFinding[];
    suggested_subject?: string;
    improvements?: string[];
    /** The rules pass, scored from the same copy in the same request. */
    rules: TemplateScore;
    /** Present when the deployment has TypeSafe configured. On a deployment
     *  with no LLM provider it is the whole analysis, and nothing is charged. */
    judgment?: CopyJudgment | null;
    model: string;
    tokens_used: number;
    charged_usd: number;
    balance_usd: number;
}
