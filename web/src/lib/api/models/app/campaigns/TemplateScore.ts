// Live content score; a hard result can stop the pre-send guardrail.
export interface TemplateScoreIssue {
    severity: "warn" | "high";
    code: string;
    message: string;
}

export default interface TemplateScore {
    score: number;
    issues: TemplateScoreIssue[];
    hard: boolean;
}

// Body for POST /templates/score.
export interface ScoreTemplateRequest {
    subject: string;
    body_html: string;
    body_plain: string;
    attachment_count?: number;
    image_count?: number;
}
