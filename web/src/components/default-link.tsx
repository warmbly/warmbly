export default function DefaultHref({ children, href, className }: { children: React.ReactNode, href: string, className?: string }) {
    return <a className={`text-primary hover:underline${className ? ` ${className}` : ""}`} href={href}>
        {children}
    </a>
}
