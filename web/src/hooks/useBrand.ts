import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { API_URL } from "@/lib/information";
import type { DeploymentBrand } from "@/lib/api/models/auth/AuthConfig";

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
export default function useBrand(): DeploymentBrand & { apiURL: string } {
    const { config } = useAuthConfig();
    return {
        name: config.brand?.name || "Cloudsnow",
        website_url: config.brand?.website_url,
        website_label: config.brand?.website_label,
        terms_url: config.brand?.terms_url,
        privacy_url: config.brand?.privacy_url,
        support_email: config.brand?.support_email,
        apiURL: config.api_url || API_URL,
    };
}
