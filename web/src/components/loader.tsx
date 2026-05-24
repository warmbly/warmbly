export function Loading({ className, color }: { className: string, color?: string }) {
    return <svg className={`loading ${className}`} viewBox="25 25 50 50">
        <circle r="20" cy="50" cx="50" stroke={`${color ? color : "white"}`}></circle>
    </svg>
}
