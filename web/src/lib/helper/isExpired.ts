export default function isExpired(d: Date): boolean {
    return d.getTime() <= Date.now()
}
