export default interface ActiveSession {
    id: string;
    current: boolean;
    browser: string;
    os: string;
    location_city: string;
    location_region: string;
    location_country: string;
    country_code: string;
    auth_provider: string;
    created_at: Date;
    last_active_at: Date;
}
