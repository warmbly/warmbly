import type Service from "./Service";

export default interface AddEmail {
    email: string;
    name: string;
    smtp: Service;
    imap: Service;
}
