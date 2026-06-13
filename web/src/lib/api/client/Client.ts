import axios from "axios";
import { API_BASE_URL } from "@/lib/information";
import { normalizeError } from "./normalizeError";

const Client = axios.create({
    baseURL: API_BASE_URL,
})

Client.interceptors.response.use(
    (response) => response,
    (error) => {
        throw normalizeError(error);
    }
);

export default Client;
