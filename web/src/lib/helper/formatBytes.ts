// Human-readable byte sizes for file lists (attachments, uploads, etc.).
// 1500 -> "1.5 KB", 0 -> "0 B".
export default function formatBytes(bytes: number, decimals = 1): string {
    if (!bytes || bytes <= 0) return "0 B";
    const k = 1024;
    const units = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), units.length - 1);
    const value = bytes / Math.pow(k, i);
    const dm = i === 0 ? 0 : decimals;
    return `${value.toFixed(dm)} ${units[i]}`;
}
