export function convertToValidJSONKey(key: string): string {
    const maxKeyLength = 255;

    if (key.length === 0 || key.length > maxKeyLength) {
        throw new Error(`Key "${key}" is invalid: must be between 1 and ${maxKeyLength} characters.`);
    }

    const convertedKey = key
        .toLowerCase()
        .replace(/\s+|-+/g, '_')
        .replace(/[^a-z0-9_]/g, '');

    const isValid = /^[a-z0-9_]+$/.test(convertedKey);
    if (!isValid || convertedKey.length === 0) {
        throw new Error(`Key "${key}" cannot be converted to a valid format (only lowercase letters, numbers, and underscores are allowed).`);
    }

    return convertedKey;
}

export function isValidEmail(email: string): boolean {
    const emailRegex = /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
    return emailRegex.test(email);
}

export const orgs = ["warmbly.com"]

export function isEmailFromOrganization(email: string, orgDomains: string[]): boolean {
    const match = email.match(/^[^@]+@(.+)$/);
    if (!match) return false; // invalid email
    const domain = match[1].toLowerCase();
    return orgDomains.some(orgDomain => domain === orgDomain.toLowerCase());
}
