export default function serializeDates<T>(obj: T): T {
    if (obj === null || obj === undefined) return obj

    if (obj instanceof Date) {
        return obj.toISOString() as unknown as T
    }

    if (Array.isArray(obj)) {
        return obj.map((v) => serializeDates(v)) as unknown as T
    }

    if (typeof obj === "object") {
        const entries = Object.entries(obj as Record<string, unknown>).map(
            ([key, value]) => [key, serializeDates(value)]
        )
        return Object.fromEntries(entries) as T
    }

    return obj
}
