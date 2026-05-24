import type Pagination from "../Pagination";
import type Campaign from "./Campaign";

export default interface GetCampaigns {
    data: Campaign[];
    pagination: Pagination;
}
