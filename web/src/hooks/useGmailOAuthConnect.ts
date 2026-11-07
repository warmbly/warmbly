import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";

// The backend decides; Cloud-brokered consent runs on Warmbly Cloud's client, not this instance's.
export default function useGmailOAuthConnect(viaCloud = false): boolean {
    const { config } = useAuthConfig();
    return viaCloud || config.gmail_oauth_connect === true;
}
