import { Logo } from "@/components/svg";
import { currentPortal, safeAccent } from "@/lib/portal";
import { cn } from "@/lib/utils";

/** The product's mark and name in standalone chrome: Warmbly's, or the agency's on its portal domain. */
export default function ProductMark({ logoClassName, nameClassName }: { logoClassName: string; nameClassName: string }) {
    const portal = currentPortal();
    if (!portal.portal) {
        return (
            <>
                <Logo className={logoClassName} />
                <span style={{ fontFamily: "var(--font-display)" }} className={nameClassName}>Warmbly</span>
            </>
        );
    }
    const { name, logo_url: logo } = portal.brand;
    const accent = safeAccent(portal.brand.accent_color);
    return (
        <>
            {logo ? (
                <img src={logo} alt="" className="h-6 max-w-[96px] object-contain shrink-0" />
            ) : (
                <span
                    aria-hidden
                    className="size-6 rounded-md shrink-0 inline-flex items-center justify-center text-[12px] font-bold text-white bg-slate-900"
                    style={accent ? { background: accent } : undefined}
                >
                    {(name || "?").slice(0, 1).toUpperCase()}
                </span>
            )}
            <span className={cn(nameClassName, "truncate")}>{name}</span>
        </>
    );
}
