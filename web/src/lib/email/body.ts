// Plain-text ↔ HTML conversion for email bodies.
//
// The composer is a plain textarea, so what the user typed has to be turned
// into the HTML alternative part before it goes out. Doing that with a bare
// `replace(/\n/g, "<br />")` (which is what this used to be) silently mangled
// messages: `&` came out as a broken entity, and anything the recipient's mail
// client read as a tag — `<see attached>`, `a < b`, a pasted `<div>` — swallowed
// the rest of the paragraph. Escape first, then add the markup.

const ESCAPES: Record<string, string> = {
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
};

/** Escapes the five characters that would otherwise be read as markup. */
export function escapeHtml(text: string): string {
    return text.replace(/[&<>"']/g, (c) => ESCAPES[c]);
}

// Matches a URL in already-escaped text. `<` cannot appear (it is `&lt;` by
// now), so stopping at whitespace is enough.
const URL_RE = /\bhttps?:\/\/[^\s<]+/g;

// Trailing punctuation that reads as sentence punctuation rather than part of
// the link. Unbalanced closing brackets go too: "(see https://x.com/a)".
function trimTrailingPunctuation(url: string): [string, string] {
    let end = url.length;
    while (end > 0) {
        const c = url[end - 1];
        if (".,;:!?".includes(c)) {
            end--;
            continue;
        }
        if (c === ")" && !url.slice(0, end).includes("(")) {
            end--;
            continue;
        }
        break;
    }
    return [url.slice(0, end), url.slice(end)];
}

/** Wraps bare URLs in anchors. Input must already be HTML-escaped. */
export function linkify(escaped: string): string {
    return escaped.replace(URL_RE, (match) => {
        const [url, tail] = trimTrailingPunctuation(match);
        if (!url) return match;
        return `<a href="${url}">${url}</a>${tail}`;
    });
}

/**
 * Converts composer text into the HTML body of an outgoing email.
 *
 * Line breaks become `<br />` one for one, so a blank line stays a blank line
 * and the recipient sees the paragraphs the sender typed. Runs of spaces are
 * preserved with non-breaking spaces, since HTML collapses whitespace.
 */
export function plainToHtml(text: string): string {
    if (!text) return "";
    const escaped = escapeHtml(text.replace(/\r\n/g, "\n"));
    const spaced = escaped.replace(/ {2,}/g, (run) => "&nbsp;".repeat(run.length - 1) + " ");
    return linkify(spaced).replace(/\n/g, "<br />");
}

/**
 * Renders plain text for display inside the dashboard. Same escaping rules as
 * the outbound conversion, and links open in a new tab.
 */
export function plainToDisplayHtml(text: string): string {
    return plainToHtml(text).replace(
        /<a href="/g,
        '<a target="_blank" rel="noopener noreferrer nofollow" href="',
    );
}
