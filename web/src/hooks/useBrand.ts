import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { API_URL } from "@/lib/information";
import type { DeploymentBrand } from "@/lib/api/models/auth/AuthConfig";
import { currentPortal } from "@/lib/portal";

/**
 * Who this deployment says it is, for every surface that used to hardcode
 * warmbly.com: the sign-in screen's footer links, a shared stats card, the
 * form preview's attribution, copyable API examples.
 *
 * A self-hosted instance that configured no EMAIL_BRAND_* has no website, no
 * terms and no privacy URL, and the caller must render nothing rather than a
 * link to a site with no relationship to that instance. Only `name` is always
 * present: it is the software's name, which is true of a self-host too.
 */
export default function useBrand(): DeploymentBrand & { apiURL: string; support_url?: string } {
    const { config } = useAuthConfig();
    const portal = currentPortal();
    const apiURL = config.api_url || API_URL;
    // On an agency's portal domain the agency is who the page says it is: its support and legal links replace the
    // service's. Terms and privacy fall back to the service's, which a new account still has to be shown.
    if (portal.portal) {
        const theme = portal.brand.theme ?? {};
        return {
            name: portal.brand.name,
            website_url: portal.brand.website_url,
            terms_url: theme.terms_url || config.brand?.terms_url,
            privacy_url: theme.privacy_url || config.brand?.privacy_url,
            support_email: theme.support_email,
            support_url: theme.support_url,
            apiURL,
        };
    }
    return {
        name: config.brand?.name || "Warmbly",
        website_url: config.brand?.website_url,
        website_label: config.brand?.website_label,
        terms_url: config.brand?.terms_url,
        privacy_url: config.brand?.privacy_url,
        support_email: config.brand?.support_email,
        // The backend's own answer wins; a backend that predates the field
        // leaves the dashboard's configured API origin, which is the same
        // address for every deployment that is not behind a split proxy.
        apiURL,
    };
}
