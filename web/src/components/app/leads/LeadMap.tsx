import { useEffect, useRef, useState } from "react";
import maplibregl, { type ExpressionSpecification, type GeoJSONSource, type GeoJSONSourceSpecification, type Map as MapLibreMap, type MapMouseEvent, type MapTouchEvent } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import { MailIcon, GlobeIcon, PhoneIcon, StarIcon } from "lucide-react";
import type { LatLng, LeadArea, LeadBounds, LeadPoint } from "@/lib/api/models/app/leads/Lead";
import { applyMapTheme } from "./mapTheme";
import { areaRing, boundsOf, circleBounds, circleRing, distanceM } from "./geo";

type FeatureCollection = Extract<Exclude<GeoJSONSourceSpecification["data"], string>, { type: "FeatureCollection" }>;

export type MapTool = "pan" | "rect" | "circle" | "polygon" | "lasso";
export type ColorBy = "reach" | "status" | "score" | "rating";
export type MapLayerMode = "pins" | "heat";

export interface MapView {
    center: LatLng;
    zoom: number;
    bounds: LeadBounds;
}

export interface MapControls {
    zoomBy: (delta: number) => void;
}

export interface FlyTarget {
    key: number;
    center?: LatLng;
    bounds?: LeadBounds;
    zoom?: number;
}

interface Props {
    styleUrl: string;
    dark: boolean;
    // themed lays Warmbly's palette over the style; off keeps the style's own colors.
    themed: boolean;
    controls?: React.RefObject<MapControls | null>;
    points: LeadPoint[];
    selected: Set<string>;
    activeId: string | null;
    colorBy: ColorBy;
    layer: MapLayerMode;
    area: LeadArea | null;
    tool: MapTool;
    tilt: boolean;
    initialView: { center: LatLng; zoom: number };
    fly: FlyTarget | null;
    onToolDone: () => void;
    onArea: (area: LeadArea) => void;
    onLasso: (polygon: LatLng[]) => void;
    onPick: (id: string, additive: boolean) => void;
    onView: (view: MapView) => void;
}

// Pin colors per mode, as MapLibre expressions over each feature's properties.
const COLOR: Record<ColorBy, ExpressionSpecification> = {
    reach: ["case", ["get", "has_email"], "#10b981", ["get", "has_website"], "#0ea5e9", ["get", "has_phone"], "#f59e0b", "#94a3b8"],
    status: ["match", ["get", "status"], "saved", "#8b5cf6", "contacted", "#10b981", "dismissed", "#94a3b8", "#0ea5e9"],
    score: ["interpolate", ["linear"], ["get", "score"], 0, "#cbd5e1", 35, "#7dd3fc", 70, "#0284c7", 100, "#0c4a6e"],
    rating: ["case", ["<", ["get", "rating"], 0], "#94a3b8", [">=", ["get", "rating"], 4.5], "#059669", [">=", ["get", "rating"], 4], "#10b981", [">=", ["get", "rating"], 3], "#f59e0b", "#f43f5e"],
};

function toCollection(points: LeadPoint[], selected: Set<string>, activeId: string | null): FeatureCollection {
    return {
        type: "FeatureCollection",
        features: points.map((p) => ({
            type: "Feature",
            id: p.id,
            geometry: { type: "Point", coordinates: [p.longitude, p.latitude] },
            properties: {
                id: p.id,
                status: p.status,
                score: p.score,
                rating: p.rating ?? -1,
                has_email: p.has_email,
                has_website: p.has_website,
                has_phone: p.has_phone,
                selected: selected.has(p.id),
                active: p.id === activeId,
            },
        })),
    };
}

function ringFeature(ring: [number, number][]): FeatureCollection {
    if (ring.length < 2) return { type: "FeatureCollection", features: [] };
    const closed = ring.length >= 3;
    return {
        type: "FeatureCollection",
        features: [
            closed
                ? { type: "Feature", properties: {}, geometry: { type: "Polygon", coordinates: [ring] } }
                : { type: "Feature", properties: {}, geometry: { type: "LineString", coordinates: ring } },
        ],
    };
}

const EMPTY: FeatureCollection = { type: "FeatureCollection", features: [] };

export default function LeadMap(props: Props) {
    const container = useRef<HTMLDivElement>(null);
    const mapRef = useRef<MapLibreMap | null>(null);
    const latest = useRef(props);
    latest.current = props;
    const [ready, setReady] = useState(0);
    const [hover, setHover] = useState<{ point: LeadPoint; x: number; y: number } | null>(null);
    const byId = useRef(new Map<string, LeadPoint>());
    byId.current = new Map(props.points.map((p) => [p.id, p]));

    // One map per mount; style and data changes are applied to it in place.
    useEffect(() => {
        if (!container.current) return;
        const map = new maplibregl.Map({
            container: container.current,
            style: latest.current.styleUrl,
            center: [latest.current.initialView.center.lng, latest.current.initialView.center.lat],
            zoom: latest.current.initialView.zoom,
            attributionControl: { compact: true },
            dragRotate: false,
            pitchWithRotate: false,
            maxPitch: 60,
        });
        mapRef.current = map;
        if (latest.current.controls) latest.current.controls.current = { zoomBy: (d) => map.easeTo({ zoom: map.getZoom() + d, duration: 250 }) };
        map.touchZoomRotate.disableRotation();

        const install = () => {
            const p = latest.current;
            if (p.themed) applyMapTheme(map, p.dark);
            if (!map.getSource("leads")) {
                map.addSource("leads", { type: "geojson", data: EMPTY, cluster: true, clusterRadius: 46, clusterMaxZoom: 14, promoteId: "id" });
                map.addSource("leads-flat", { type: "geojson", data: EMPTY });
                map.addSource("area", { type: "geojson", data: EMPTY });
                map.addSource("area-draft", { type: "geojson", data: EMPTY });
            }
            map.addLayer({
                id: "area-fill",
                type: "fill",
                source: "area",
                paint: { "fill-color": "#0ea5e9", "fill-opacity": 0.06 },
            });
            map.addLayer({
                id: "area-line",
                type: "line",
                source: "area",
                paint: { "line-color": "#0284c7", "line-width": 1.5, "line-dasharray": [2, 1.5] },
            });
            map.addLayer({
                id: "area-draft-fill",
                type: "fill",
                source: "area-draft",
                filter: ["==", ["geometry-type"], "Polygon"],
                paint: { "fill-color": "#0ea5e9", "fill-opacity": 0.12 },
            });
            map.addLayer({
                id: "area-draft-line",
                type: "line",
                source: "area-draft",
                paint: { "line-color": "#0369a1", "line-width": 2 },
            });
            map.addLayer({
                id: "leads-heat",
                type: "heatmap",
                source: "leads-flat",
                layout: { visibility: "none" },
                paint: {
                    "heatmap-weight": ["interpolate", ["linear"], ["get", "score"], 0, 0.2, 100, 1],
                    "heatmap-intensity": ["interpolate", ["linear"], ["zoom"], 8, 0.8, 15, 2],
                    "heatmap-radius": ["interpolate", ["linear"], ["zoom"], 8, 14, 15, 32],
                    "heatmap-opacity": 0.85,
                    "heatmap-color": [
                        "interpolate", ["linear"], ["heatmap-density"],
                        0, "rgba(14,165,233,0)", 0.2, "rgba(125,211,252,0.55)", 0.45, "#38bdf8", 0.7, "#0284c7", 1, "#f59e0b",
                    ],
                },
            });
            map.addLayer({
                id: "leads-clusters",
                type: "circle",
                source: "leads",
                filter: ["has", "point_count"],
                paint: {
                    "circle-color": ["step", ["get", "point_count"], "#38bdf8", 25, "#0ea5e9", 100, "#0369a1"],
                    "circle-radius": ["step", ["get", "point_count"], 15, 25, 19, 100, 24],
                    "circle-stroke-width": 3,
                    "circle-stroke-color": p.dark ? "rgba(11,18,32,0.75)" : "rgba(255,255,255,0.85)",
                },
            });
            map.addLayer({
                id: "leads-cluster-count",
                type: "symbol",
                source: "leads",
                filter: ["has", "point_count"],
                layout: { "text-field": ["get", "point_count_abbreviated"], "text-size": 11, "text-font": ["Noto Sans Bold"], "text-allow-overlap": true },
                paint: { "text-color": "#ffffff" },
            });
            map.addLayer({
                id: "leads-halo",
                type: "circle",
                source: "leads",
                filter: ["all", ["!", ["has", "point_count"]], ["any", ["get", "selected"], ["get", "active"]]],
                paint: {
                    "circle-radius": ["case", ["get", "active"], 13, 10],
                    "circle-color": ["case", ["get", "active"], "rgba(14,165,233,0.28)", "rgba(14,165,233,0.18)"],
                    "circle-stroke-width": 1.5,
                    "circle-stroke-color": "#0284c7",
                },
            });
            map.addLayer({
                id: "leads-points",
                type: "circle",
                source: "leads",
                filter: ["!", ["has", "point_count"]],
                paint: {
                    "circle-color": COLOR[p.colorBy],
                    "circle-radius": ["interpolate", ["linear"], ["zoom"], 9, 4, 14, 6.5, 18, 8],
                    "circle-stroke-width": 2,
                    "circle-stroke-color": p.dark ? "#0b1220" : "#ffffff",
                },
            });
            setReady((n) => n + 1);
        };
        map.on("style.load", install);
        return () => {
            map.remove();
            mapRef.current = null;
        };
    }, []);

    // Style switches drop every layer; style.load reinstalls them.
    const firstStyle = useRef(true);
    useEffect(() => {
        if (firstStyle.current) {
            firstStyle.current = false;
            return;
        }
        mapRef.current?.setStyle(props.styleUrl);
    }, [props.styleUrl]);

    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready) return;
        if (props.themed) applyMapTheme(map, props.dark);
        const stroke = props.dark ? "#0b1220" : "#ffffff";
        if (map.getLayer("leads-points")) map.setPaintProperty("leads-points", "circle-stroke-color", stroke);
    }, [props.dark, props.themed, ready]);

    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready) return;
        const data = toCollection(props.points, props.selected, props.activeId);
        (map.getSource("leads") as GeoJSONSource | undefined)?.setData(data);
        (map.getSource("leads-flat") as GeoJSONSource | undefined)?.setData(data);
    }, [props.points, props.selected, props.activeId, ready]);

    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready || !map.getLayer("leads-points")) return;
        map.setPaintProperty("leads-points", "circle-color", COLOR[props.colorBy]);
    }, [props.colorBy, ready]);

    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready) return;
        const heat = props.layer === "heat";
        for (const id of ["leads-clusters", "leads-cluster-count", "leads-points", "leads-halo"]) {
            if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", heat ? "none" : "visible");
        }
        if (map.getLayer("leads-heat")) map.setLayoutProperty("leads-heat", "visibility", heat ? "visible" : "none");
    }, [props.layer, ready]);

    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready) return;
        (map.getSource("area") as GeoJSONSource | undefined)?.setData(props.area ? ringFeature(areaRing(props.area)) : EMPTY);
    }, [props.area, ready]);

    useEffect(() => {
        mapRef.current?.easeTo({ pitch: props.tilt ? 50 : 0, duration: 600 });
    }, [props.tilt]);

    useEffect(() => {
        const map = mapRef.current;
        const f = props.fly;
        if (!map || !f) return;
        if (f.bounds) {
            map.fitBounds([[f.bounds.west, f.bounds.south], [f.bounds.east, f.bounds.north]], { padding: 64, maxZoom: 16, duration: 900 });
        } else if (f.center) {
            map.flyTo({ center: [f.center.lng, f.center.lat], zoom: f.zoom ?? Math.max(map.getZoom(), 14), duration: 900 });
        }
    }, [props.fly]);

    // Clicks, hovers and the view report back through the latest props.
    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready) return;
        const report = () => {
            const b = map.getBounds();
            const c = map.getCenter();
            latest.current.onView({
                center: { lat: c.lat, lng: c.lng },
                zoom: map.getZoom(),
                bounds: { south: b.getSouth(), west: b.getWest(), north: b.getNorth(), east: b.getEast() },
            });
        };
        report();
        const onClusterClick = async (e: MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
            const f = e.features?.[0];
            if (!f || latest.current.tool !== "pan") return;
            const src = map.getSource("leads") as GeoJSONSource;
            const zoom = await src.getClusterExpansionZoom(f.properties.cluster_id as number);
            const [lng, lat] = (f.geometry as unknown as { coordinates: [number, number] }).coordinates;
            map.easeTo({ center: [lng, lat], zoom: zoom + 0.5 });
        };
        const onPointClick = (e: MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
            const f = e.features?.[0];
            if (!f || latest.current.tool !== "pan") return;
            const ev = e.originalEvent as MouseEvent;
            latest.current.onPick(String(f.properties.id), ev.shiftKey || ev.metaKey || ev.ctrlKey);
        };
        const onMove = (e: MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
            const f = e.features?.[0];
            const pt = f ? byId.current.get(String(f.properties.id)) : undefined;
            if (pt && latest.current.tool === "pan") setHover({ point: pt, x: e.point.x, y: e.point.y });
        };
        const enter = () => {
            if (latest.current.tool === "pan") map.getCanvas().style.cursor = "pointer";
        };
        const leave = () => {
            if (latest.current.tool === "pan") map.getCanvas().style.cursor = "";
            setHover(null);
        };
        map.on("moveend", report);
        map.on("click", "leads-clusters", onClusterClick);
        map.on("click", "leads-points", onPointClick);
        map.on("mousemove", "leads-points", onMove);
        map.on("mouseenter", "leads-points", enter);
        map.on("mouseleave", "leads-points", leave);
        map.on("mouseenter", "leads-clusters", enter);
        map.on("mouseleave", "leads-clusters", leave);
        map.on("movestart", () => setHover(null));
        return () => {
            map.off("moveend", report);
            map.off("click", "leads-clusters", onClusterClick);
            map.off("click", "leads-points", onPointClick);
            map.off("mousemove", "leads-points", onMove);
            map.off("mouseenter", "leads-points", enter);
            map.off("mouseleave", "leads-points", leave);
            map.off("mouseenter", "leads-clusters", enter);
            map.off("mouseleave", "leads-clusters", leave);
        };
    }, [ready]);

    // Drawing: rectangle and circle drag, polygon clicks, lasso freehand.
    useEffect(() => {
        const map = mapRef.current;
        if (!map || !ready) return;
        const tool = props.tool;
        const draft = map.getSource("area-draft") as GeoJSONSource;
        const clear = () => draft.setData(EMPTY);
        if (tool === "pan") {
            map.dragPan.enable();
            map.doubleClickZoom.enable();
            map.getCanvas().style.cursor = "";
            clear();
            return;
        }
        map.dragPan.disable();
        map.doubleClickZoom.disable();
        map.getCanvas().style.cursor = "crosshair";
        setHover(null);

        let start: maplibregl.LngLat | null = null;
        let lassoPts: LatLng[] = [];
        let poly: LatLng[] = [];
        let lastPx: { x: number; y: number } | null = null;

        const pos = (e: MapMouseEvent | MapTouchEvent) => e.lngLat;
        const down = (e: MapMouseEvent | MapTouchEvent) => {
            if (tool === "polygon") return;
            e.preventDefault();
            start = pos(e);
            lassoPts = [{ lat: start.lat, lng: start.lng }];
            lastPx = { x: e.point.x, y: e.point.y };
        };
        const move = (e: MapMouseEvent | MapTouchEvent) => {
            const cur = pos(e);
            if (tool === "polygon") {
                if (poly.length) draft.setData(ringFeature([...poly, { lat: cur.lat, lng: cur.lng }].map((p) => [p.lng, p.lat] as [number, number])));
                return;
            }
            if (!start) return;
            if (tool === "rect") {
                const b = boundsOf([{ lat: start.lat, lng: start.lng }, { lat: cur.lat, lng: cur.lng }]);
                draft.setData(ringFeature([[b.west, b.south], [b.east, b.south], [b.east, b.north], [b.west, b.north], [b.west, b.south]]));
            } else if (tool === "circle") {
                const r = distanceM({ lat: start.lat, lng: start.lng }, { lat: cur.lat, lng: cur.lng });
                draft.setData(ringFeature(circleRing({ lat: start.lat, lng: start.lng }, Math.max(r, 1))));
            } else if (tool === "lasso" && lastPx && Math.hypot(e.point.x - lastPx.x, e.point.y - lastPx.y) > 6) {
                lastPx = { x: e.point.x, y: e.point.y };
                lassoPts.push({ lat: cur.lat, lng: cur.lng });
                draft.setData(ringFeature([...lassoPts, lassoPts[0]].map((p) => [p.lng, p.lat] as [number, number])));
            }
        };
        const up = (e: MapMouseEvent | MapTouchEvent) => {
            if (tool === "polygon" || !start) return;
            const cur = pos(e);
            const s = { lat: start.lat, lng: start.lng };
            const c = { lat: cur.lat, lng: cur.lng };
            start = null;
            clear();
            const done = latest.current;
            if (tool === "rect") {
                const b = boundsOf([s, c]);
                if (b.north - b.south > 1e-4 && b.east - b.west > 1e-4) done.onArea({ kind: "bbox", bounds: b });
            } else if (tool === "circle") {
                const r = Math.min(distanceM(s, c), 50000);
                if (r >= 50) done.onArea({ kind: "circle", center: s, radius_m: Math.round(r), bounds: circleBounds(s, r) });
            } else if (tool === "lasso" && lassoPts.length >= 3) {
                done.onLasso(lassoPts);
            }
            done.onToolDone();
        };
        const click = (e: MapMouseEvent) => {
            if (tool !== "polygon") return;
            const p = { lat: e.lngLat.lat, lng: e.lngLat.lng };
            if (poly.length >= 3) {
                const first = map.project([poly[0].lng, poly[0].lat]);
                if (Math.hypot(first.x - e.point.x, first.y - e.point.y) < 12) return finish();
            }
            poly.push(p);
            draft.setData(ringFeature(poly.map((q) => [q.lng, q.lat] as [number, number])));
        };
        const finish = () => {
            if (poly.length >= 3) latest.current.onArea({ kind: "polygon", polygon: poly, bounds: boundsOf(poly) });
            poly = [];
            clear();
            latest.current.onToolDone();
        };
        const dbl = (e: MapMouseEvent) => {
            if (tool !== "polygon") return;
            e.preventDefault();
            finish();
        };
        const key = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                poly = [];
                start = null;
                clear();
                latest.current.onToolDone();
            } else if (e.key === "Enter" && tool === "polygon") {
                finish();
            }
        };
        map.on("mousedown", down);
        map.on("touchstart", down);
        map.on("mousemove", move);
        map.on("touchmove", move);
        map.on("mouseup", up);
        map.on("touchend", up);
        map.on("click", click);
        map.on("dblclick", dbl);
        window.addEventListener("keydown", key);
        return () => {
            map.off("mousedown", down);
            map.off("touchstart", down);
            map.off("mousemove", move);
            map.off("touchmove", move);
            map.off("mouseup", up);
            map.off("touchend", up);
            map.off("click", click);
            map.off("dblclick", dbl);
            window.removeEventListener("keydown", key);
            clear();
        };
    }, [props.tool, ready]);

    return (
        <div className="absolute inset-0">
            {/* MapLibre makes its container position: relative, so size it explicitly. */}
            <div ref={container} className="h-full w-full" />
            {hover && <HoverCard point={hover.point} x={hover.x} y={hover.y} />}
        </div>
    );
}

function HoverCard({ point, x, y }: { point: LeadPoint; x: number; y: number }) {
    return (
        <div
            className="pointer-events-none absolute z-10 w-[220px] rounded-lg border border-slate-200 bg-white/95 px-2.5 py-2 shadow-[0_8px_24px_-12px_rgba(15,23,42,0.35)] backdrop-blur"
            style={{ left: x, top: y - 14, transform: "translate(-50%, -100%)" }}
        >
            <div className="truncate text-[12.5px] font-medium text-slate-900">{point.name}</div>
            {point.category && <div className="truncate text-[11px] text-slate-500">{point.category}</div>}
            <div className="mt-1.5 flex items-center gap-2 text-[11px] text-slate-500">
                <MailIcon className={point.has_email ? "size-3 text-emerald-600" : "size-3 text-slate-300"} />
                <GlobeIcon className={point.has_website ? "size-3 text-sky-600" : "size-3 text-slate-300"} />
                <PhoneIcon className={point.has_phone ? "size-3 text-amber-600" : "size-3 text-slate-300"} />
                {point.rating != null && (
                    <span className="inline-flex items-center gap-0.5">
                        <StarIcon className="size-3 fill-amber-400 text-amber-400" />
                        {point.rating.toFixed(1)}
                        {point.review_count != null && <span className="text-slate-400">({point.review_count})</span>}
                    </span>
                )}
                <span className="ml-auto tabular-nums text-slate-400">{point.score}</span>
            </div>
        </div>
    );
}
