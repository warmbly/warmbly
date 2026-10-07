import React from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import SequenceView from "./SequenceView";
import { EmailsStep } from "../new/steps";
import { initialDraft, type Draft } from "../new/draft";
import type { Patch } from "../new/fields";

const f = vi.hoisted(() => ({
    save: vi.fn(),
    confirm: vi.fn(),
    templates: [{ id: "template-id", name: "Cold open", subject: "Hello {{.FirstName}}", body_html: "<p>Template body</p>", body_plain: "Template body" }],
}));

vi.mock("@/lib/api/hooks/app/templates/useTemplates", () => ({ default: () => ({ data: f.templates }) }));
vi.mock("@/lib/api/hooks/app/templates/useCreateTemplate", () => ({ default: () => ({ isPending: false }) }));
vi.mock("@/lib/api/hooks/app/campaigns/sequences/useUpdateSequence", () => ({ default: () => ({ mutateAsync: f.save }) }));
vi.mock("@/lib/api/hooks/app/campaigns/useCampaign", () => ({ default: () => ({ data: undefined }) }));
vi.mock("@/lib/api/hooks/app/campaigns/useTemplatePreview", () => ({ useTemplatePreview: () => ({ mutateAsync: vi.fn() }) }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: f.confirm }) }));
vi.mock("@/components/app/flow/dirty", () => ({ useReportDirty: () => undefined }));
vi.mock("./previewContext", () => ({
    useCampaignSenderInboxes: () => ({ inboxes: [] }),
    SAMPLE_CONTACT_LABEL: "Sample contact",
    contactLabel: () => "Contact",
}));
vi.mock("./RichTextEditor", () => ({
    default: ({ html }: { html: string }) => <output aria-label="Email body">{html}</output>,
    VariableMenu: () => null,
}));
vi.mock("./StepAttachments", () => ({ default: () => null }));
vi.mock("@/components/ui/scroll-strip", () => ({ default: ({ children }: { children: React.ReactNode }) => <div>{children}</div> }));
vi.mock("../ContentScore", () => ({ default: () => null }));
vi.mock("./PreviewControls", () => ({ PlacementTestButton: () => null, PreviewContactPicker: () => null, PreviewMailboxPicker: () => null, SendTestButton: () => null }));

const step: Sequence = {
    id: "step-id", name: "Opening email", subject: "", body_html: "", body_plain: "", body_code: false,
    body_sync: true, thread_reply: true, kind: "email", wait_after: 0, x: 0, y: 0,
    created_at: new Date(0), updated_at: new Date(0),
};

async function selectTemplate() {
    fireEvent.click(screen.getByRole("button", { name: "Templates" }));
    fireEvent.click(await screen.findByRole("button", { name: /Cold open/ }));
}

function NewCampaignEmails({ initial, selected = 0 }: { initial?: Draft; selected?: number }) {
    const [draft, setDraft] = React.useState(() => initial ?? initialDraft("UTC"));
    const patch: Patch = (p) => setDraft((d) => ({ ...d, ...(typeof p === "function" ? p(d) : p) }));
    return <>
        <EmailsStep draft={draft} patch={patch} selected={selected} setSelected={() => undefined} />
        <output aria-label="New campaign subject">{draft.emails[selected].subject}</output>
        <output aria-label="New campaign body">{draft.emails[selected].body_html}</output>
        <output aria-label="New campaign emails">{JSON.stringify(draft.emails)}</output>
    </>;
}

describe("campaign template application", () => {
    beforeEach(() => {
        f.save.mockReset().mockResolvedValue(undefined);
        f.confirm.mockReset();
        f.templates[0].subject = "Hello {{.FirstName}}";
    });

    it("retains both template fields in the new campaign wizard's draft", async () => {
        render(<NewCampaignEmails />);
        await selectTemplate();
        expect(screen.getByLabelText("New campaign subject")).toHaveTextContent("Hello {{.FirstName}}");
        expect(screen.getByLabelText("New campaign body")).toHaveTextContent("<p>Template body</p>");
    });

    it("replaces both wizard fields after confirmation rather than restoring the old subject", async () => {
        const initial = initialDraft("UTC");
        initial.emails[0] = { ...initial.emails[0], subject: "Original subject", body_html: "<p>Original body</p>" };
        render(<NewCampaignEmails initial={initial} />);
        await selectTemplate();
        expect(screen.getByLabelText("New campaign subject")).toHaveTextContent("Original subject");
        act(() => f.confirm.mock.calls[0][1]());
        expect(screen.getByLabelText("New campaign subject")).toHaveTextContent("Hello {{.FirstName}}");
        expect(screen.getByLabelText("New campaign body")).toHaveTextContent("<p>Template body</p>");
    });

    it("applies the template only to the selected wizard email, preserving the other draft emails", async () => {
        const initial = initialDraft("UTC");
        initial.emails[0].subject = "Keep opener";
        initial.emails.push({ ...initial.emails[0], id: "follow-up", subject: "", wait_after: 3 });
        render(<NewCampaignEmails initial={initial} selected={1} />);
        await selectTemplate();
        expect(screen.getByLabelText("New campaign subject")).toHaveTextContent("Hello {{.FirstName}}");
        expect(screen.getByLabelText("New campaign body")).toHaveTextContent("<p>Template body</p>");
        const emails = JSON.parse(screen.getByLabelText("New campaign emails").textContent!);
        expect(emails[0]).toEqual(initial.emails[0]);
        expect(emails[1].wait_after).toBe(3);
    });

    it("copies and saves the subject and body together on a new campaign opening step", async () => {
        render(<SequenceView campaignId="campaign-id" sequence={step} index={0} />);
        await selectTemplate();
        expect(screen.getByPlaceholderText("Quick question, {{.FirstName}}")).toHaveValue("Hello {{.FirstName}}");
        expect(screen.getByLabelText("Email body")).toHaveTextContent("<p>Template body</p>");
        expect(f.confirm).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(f.save).toHaveBeenCalledWith({ subject: "Hello {{.FirstName}}", body_html: "<p>Template body</p>", body_plain: "Template body" }));
    });

    it("waits for confirmation before replacing an existing subject and body", async () => {
        render(<SequenceView campaignId="campaign-id" sequence={{ ...step, subject: "Original subject", body_html: "<p>Original body</p>" }} index={0} />);
        await selectTemplate();
        expect(f.confirm).toHaveBeenCalledOnce();
        expect(screen.getByPlaceholderText("Quick question, {{.FirstName}}")).toHaveValue("Original subject");
        expect(screen.getByLabelText("Email body")).toHaveTextContent("<p>Original body</p>");
        act(() => f.confirm.mock.calls[0][1]());
        expect(screen.getByPlaceholderText("Quick question, {{.FirstName}}")).toHaveValue("Hello {{.FirstName}}");
        expect(screen.getByLabelText("Email body")).toHaveTextContent("<p>Template body</p>");
    });

    it("keeps an existing subject when the selected template has none", async () => {
        f.templates[0].subject = "";
        render(<SequenceView campaignId="campaign-id" sequence={{ ...step, subject: "Keep this subject" }} index={0} />);
        await selectTemplate();
        act(() => f.confirm.mock.calls[0][1]());
        expect(screen.getByPlaceholderText("Quick question, {{.FirstName}}")).toHaveValue("Keep this subject");
        expect(screen.getByLabelText("Email body")).toHaveTextContent("<p>Template body</p>");
    });

    it("keeps a follow-up's inherited conversation subject and only saves its body", async () => {
        render(<SequenceView campaignId="campaign-id" sequence={step} index={1} conversationSubject="Conversation opener" />);
        await selectTemplate();
        act(() => f.confirm.mock.calls[0][1]());
        expect(screen.queryByPlaceholderText("Quick question, {{.FirstName}}")).not.toBeInTheDocument();
        expect(screen.getByText("Conversation opener")).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
        await waitFor(() => expect(f.save).toHaveBeenCalledWith({ body_html: "<p>Template body</p>", body_plain: "Template body" }));
    });
});
