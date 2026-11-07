import type { LatLng, LeadArea, LeadBounds } from "@/lib/api/models/app/leads/Lead";

const EARTH_M = 6371000;

export function distanceM(a: LatLng, b: LatLng): number {
    const la1 = (a.lat * Math.PI) / 180;
    const la2 = (b.lat * Math.PI) / 180;
    const dLat = la2 - la1;
    const dLng = ((b.lng - a.lng) * Math.PI) / 180;
    const h = Math.sin(dLat / 2) ** 2 + Math.cos(la1) * Math.cos(la2) * Math.sin(dLng / 2) ** 2;
    return 2 * EARTH_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

// circleRing approximates a circle as a closed ring of [lng, lat] points.
export function circleRing(center: LatLng, radiusM: number, steps = 72): [number, number][] {
    const out: [number, number][] = [];
    const lat = (center.lat * Math.PI) / 180;
    const lng = (center.lng * Math.PI) / 180;
    const d = radiusM / EARTH_M;
    for (let i = 0; i <= steps; i++) {
        const brg = (i / steps) * 2 * Math.PI;
        const la = Math.asin(Math.sin(lat) * Math.cos(d) + Math.cos(lat) * Math.sin(d) * Math.cos(brg));
        const lo = lng + Math.atan2(Math.sin(brg) * Math.sin(d) * Math.cos(lat), Math.cos(d) - Math.sin(lat) * Math.sin(la));
        out.push([(lo * 180) / Math.PI, (la * 180) / Math.PI]);
    }
    return out;
}

export function boundsOf(points: LatLng[]): LeadBounds {
    const b = { south: 90, north: -90, west: 180, east: -180 };
    for (const p of points) {
        b.south = Math.min(b.south, p.lat);
        b.north = Math.max(b.north, p.lat);
        b.west = Math.min(b.west, p.lng);
        b.east = Math.max(b.east, p.lng);
    }
    return b;
}

export function circleBounds(center: LatLng, radiusM: number): LeadBounds {
    return boundsOf(circleRing(center, radiusM, 16).map(([lng, lat]) => ({ lat, lng })));
}

export function inPolygon(poly: LatLng[], p: LatLng): boolean {
    let inside = false;
    for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
        const a = poly[i];
        const b = poly[j];
        if (a.lat > p.lat !== b.lat > p.lat && p.lng < ((b.lng - a.lng) * (p.lat - a.lat)) / (b.lat - a.lat) + a.lng) inside = !inside;
    }
    return inside;
}

export function areaContains(area: LeadArea, p: LatLng): boolean {
    if (area.kind === "circle") return distanceM(area.center, p) <= area.radius_m;
    if (area.kind === "polygon") return inPolygon(area.polygon, p);
    const b = area.bounds;
    return p.lat >= b.south && p.lat <= b.north && p.lng >= b.west && p.lng <= b.east;
}

// areaRing is the shape drawn on the map for an area, as [lng, lat] points.
export function areaRing(area: LeadArea): [number, number][] {
    if (area.kind === "circle") return circleRing(area.center, area.radius_m);
    if (area.kind === "polygon") {
        const ring = area.polygon.map((p) => [p.lng, p.lat] as [number, number]);
        return ring.length ? [...ring, ring[0]] : ring;
    }
    const b = area.bounds;
    return [
        [b.west, b.south],
        [b.east, b.south],
        [b.east, b.north],
        [b.west, b.north],
        [b.west, b.south],
    ];
}

export function areaKm2(b: LeadBounds): number {
    const mid = (((b.south + b.north) / 2) * Math.PI) / 180;
    const h = (((b.north - b.south) * Math.PI) / 180) * EARTH_M;
    const w = (((b.east - b.west) * Math.PI) / 180) * EARTH_M * Math.cos(mid);
    return (h * w) / 1e6;
}

export function describeArea(area: LeadArea): string {
    if (area.kind === "circle") {
        const r = area.radius_m;
        return `Circle, ${r >= 1000 ? `${(r / 1000).toFixed(r >= 10000 ? 0 : 1)} km` : `${Math.round(r)} m`} radius`;
    }
    const km = areaKm2(area.bounds);
    const size = km >= 10 ? `${Math.round(km)} km²` : `${km.toFixed(1)} km²`;
    return area.kind === "polygon" ? `Drawn area, ${size}` : `Map view, ${size}`;
}
