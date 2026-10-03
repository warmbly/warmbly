// The Warmbly geometric mark. Single-path, inherits color via currentColor so
// it works on light or dark surfaces. Mirrors the wordmark used on the
// marketing site and dashboard so the admin app shares one brand.

export function Logo({ className }: { className?: string }) {
    return (
        <img
            src="/logo.avif"
            alt="Cloudsnow"
            className={className ? `${className} object-contain shrink-0` : "size-6 object-contain shrink-0"}
        />
    );
}
