export default function reviveDates<T>(obj: T): T {
    if (obj === null || obj === undefined) return obj

    if (typeof obj === "string" && /^\d{4}-\d{2}-\d{2}T/.test(obj)) {
        return new Date(obj) as unknown as T
    }

    if (Array.isArray(obj)) {
        return obj.map((v) => reviveDates(v)) as unknown as T
    }

    if (typeof obj === "object") {
        const entries = Object.entries(obj as Record<string, unknown>).map(
            ([key, value]) => [key, reviveDates(value)]
        )
        return Object.fromEntries(entries) as T
    }

    return obj
}
