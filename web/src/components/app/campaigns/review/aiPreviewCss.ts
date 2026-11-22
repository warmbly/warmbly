// Styles inside the email preview. Written AI copy carries a soft highlighter
// so a reviewer sees what AI wrote; copy being written is a shimmering line
// pair; copy just written fades in. Unwritten copy a member cannot write
// shows as a quiet labelled slot.
export function aiPreviewCss({ writing, justWritten }: { writing: boolean; justWritten: string[] }): string {
    const fresh = justWritten.map((id) => `[data-ai-block="${CSS.escape(id)}"]`).join(",");
    return `
  [data-ai-block] {
    background: linear-gradient(transparent 58%, rgba(14, 165, 233, 0.13) 58%);
    border-radius: 2px;
    box-decoration-break: clone;
    -webkit-box-decoration-break: clone;
  }
  [data-ai-pending] {
    display: block;
    position: relative;
    width: min(100%, 460px);
    height: 0.75em;
    margin: 0.5em 0 2em;
    border-radius: 4px;
    background: #f1f5f9;
  }
  [data-ai-pending]::after {
    content: "";
    position: absolute;
    left: 0;
    top: 1.3em;
    width: 62%;
    height: 100%;
    border-radius: 4px;
    background: inherit;
  }
  ${
      writing
          ? `
  [data-ai-pending], [data-ai-pending]::after {
    background: linear-gradient(90deg, #eef2f7 0%, #eef2f7 35%, #dbeefc 50%, #eef2f7 65%, #eef2f7 100%);
    background-size: 220% 100%;
    animation: ai-writing-sweep 1.3s ease-in-out infinite;
  }
`
          : `
  [data-ai-pending] {
    display: inline-block; width: auto; height: auto; padding: 1px 8px; margin: 0;
    border: 1px dashed #cbd5e1; background: transparent;
    color: #64748b; font-size: 12px;
  }
  [data-ai-pending]::before { content: "AI " attr(data-ai-name); }
  [data-ai-pending]::after { display: none; }`
  }
  ${
      fresh
          ? `
  ${fresh} { animation: ai-reveal 1.1s ease-out both; }`
          : ""
  }
  @keyframes ai-writing-sweep { from { background-position: 160% 0; } to { background-position: -60% 0; } }
  @keyframes ai-reveal {
    from { opacity: 0; filter: blur(3px); background-color: rgba(14, 165, 233, 0.22); }
    60% { opacity: 1; filter: blur(0); }
    to { background-color: transparent; }
  }
  @media (prefers-reduced-motion: reduce) {
    [data-ai-pending], [data-ai-pending]::after, [data-ai-block] { animation: none !important; }
  }
`;
}
