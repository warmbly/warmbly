// Result of POST /templates/score — an advisory content-quality score for a
// campaign template. Higher score = safer; issues are non-blocking hints to
// improve deliverability (the score never prevents saving or sending).
export interface TemplateScoreIssue {
    severity: "warn" | "high";
    code: string;
    message: string;
}

export default interface TemplateScore {
    score: number;
    issues: TemplateScoreIssue[];
}

// Body for POST /templates/score.
export interface ScoreTemplateRequest {
    subject: string;
    body_html: string;
    body_plain: string;
}
