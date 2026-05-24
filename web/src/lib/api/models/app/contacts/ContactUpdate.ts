import type Contact from "./Contact";

export default interface ContactUpdate extends Omit<Contact, "campaigns"> {
    campaigns: string[],
}
