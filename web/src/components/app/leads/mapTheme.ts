import type { Map as MapLibreMap } from "maplibre-gl";

// Warmbly's palette laid over any OpenMapTiles-shaped vector style, so the map
// reads as part of the dashboard rather than a stock basemap.
interface Palette {
    background: string;
    water: string;
    waterLine: string;
    green: string;
    building: string;
    road: string;
    roadMajor: string;
    casing: string;
    boundary: string;
    label: string;
    labelHalo: string;
}

const LIGHT: Palette = {
    background: "#f8fafc",
    water: "#dbeefb",
    waterLine: "#bfdcf3",
    green: "#e8f5ee",
    building: "#eef1f5",
    road: "#ffffff",
    roadMajor: "#ffffff",
    casing: "#e2e8f0",
    boundary: "#cbd5e1",
    label: "#64748b",
    labelHalo: "#f8fafc",
};

const DARK: Palette = {
    background: "#0b1220",
    water: "#0d2236",
    waterLine: "#12304a",
    green: "#0e1f1a",
    building: "#131c2b",
    road: "#1b2537",
    roadMajor: "#243149",
    casing: "#0b1220",
    boundary: "#334155",
    label: "#8a9ab0",
    labelHalo: "#0b1220",
};

function set(map: MapLibreMap, id: string, prop: string, value: unknown) {
    try {
        map.setPaintProperty(id, prop, value);
    } catch {
        // A layer without the property keeps its own.
    }
}

export function applyMapTheme(map: MapLibreMap, dark: boolean) {
    const p = dark ? DARK : LIGHT;
    const layers = map.getStyle()?.layers ?? [];
    for (const layer of layers) {
        const id = layer.id;
        if (id.startsWith("leads-") || id.startsWith("area-")) continue;
        const name = id.toLowerCase();
        switch (layer.type) {
            case "background":
                set(map, id, "background-color", p.background);
                break;
            case "fill":
                if (/water|ocean|lake/.test(name)) set(map, id, "fill-color", p.water);
                else if (/park|wood|grass|forest|landcover|green|cemetery|pitch/.test(name)) set(map, id, "fill-color", p.green);
                else if (/building/.test(name)) set(map, id, "fill-color", p.building);
                else if (/landuse|residential|aeroway|industrial/.test(name)) set(map, id, "fill-color", p.background);
                break;
            case "fill-extrusion":
                set(map, id, "fill-extrusion-color", p.building);
                break;
            case "line":
                if (/water|river|stream|canal/.test(name)) set(map, id, "line-color", p.waterLine);
                else if (/boundary|admin/.test(name)) set(map, id, "line-color", p.boundary);
                else if (/casing|outline/.test(name)) set(map, id, "line-color", p.casing);
                else if (/motorway|trunk|primary|major/.test(name)) set(map, id, "line-color", p.roadMajor);
                else if (/road|highway|street|minor|secondary|tertiary|service|path|track|bridge|tunnel/.test(name)) set(map, id, "line-color", p.road);
                break;
            case "symbol":
                // Other businesses' icons compete with the pins; names stay.
                if (/poi/.test(name)) {
                    try {
                        map.setLayoutProperty(id, "visibility", "none");
                    } catch {
                        // Keep a layer that refuses to hide.
                    }
                    break;
                }
                set(map, id, "text-color", p.label);
                set(map, id, "text-halo-color", p.labelHalo);
                break;
        }
    }
}
