import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import type { GlyphSize } from "@/app/app/integrations/_components/glyphSize";
import type { LeadProvider } from "@/lib/api/models/app/leads/Lead";

// OpenStreetMap is not an integration, so its mark is filed under its own name.
const GLYPH: Record<LeadProvider, string> = { osm: "openstreetmap", google_places: "google_places", foursquare: "foursquare", outscraper: "outscraper" };

export default function SourceLogo({ provider, name, size = 5 }: { provider: LeadProvider; name: string; size?: GlyphSize }) {
    return <ProviderGlyph provider={GLYPH[provider]} name={name} size={size} />;
}
