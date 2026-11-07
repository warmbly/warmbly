// Logo tile sizes shared by built-in marks and community app logos.

export type GlyphSize = 5 | 7 | 9 | 10 | 12;

// Larger tiles round a little more so an app page header mark reads as an icon.
export const GLYPH_DIMS: Record<GlyphSize, { tile: string; icon: string; text: string }> = {
    5: { tile: "w-5 h-5 rounded", icon: "w-3.5 h-3.5", text: "text-[10px]" },
    7: { tile: "w-7 h-7 rounded-md", icon: "w-4 h-4", text: "text-[12px]" },
    9: { tile: "w-9 h-9 rounded-md", icon: "w-5 h-5", text: "text-[13px]" },
    10: { tile: "w-10 h-10 rounded-lg", icon: "w-6 h-6", text: "text-[15px]" },
    12: { tile: "w-12 h-12 rounded-lg", icon: "w-7 h-7", text: "text-[18px]" },
};
