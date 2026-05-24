export default interface AddContact {
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    campaigns: string[];

    custom_fields: Record<string, string>;
}
