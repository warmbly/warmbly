import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Turn a "#rrggbb" (or "#rgb") hex into a translucent rgba() string — handy for
// tinting a UI surface with a user-chosen color. Falls back to slate on a
// malformed hex so callers never render an invalid background.
export function hexToRgba(hex: string, alpha: number): string {
  const h = (hex ?? "").replace("#", "")
  const full = h.length === 3 ? h.split("").map((c) => c + c).join("") : h
  const n = parseInt(full, 16)
  if (full.length !== 6 || Number.isNaN(n)) return `rgba(100,116,139,${alpha})`
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${alpha})`
}
