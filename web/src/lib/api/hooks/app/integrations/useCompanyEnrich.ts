import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import getConnection from "@/lib/api/client/app/integrations/getConnection";
import { enrichCompanyEnrichContact, queryCompanyEnrich } from "@/lib/api/client/app/integrations/companyenrich";
import type { CompanyEnrichContactInput, CompanyEnrichQuery } from "@/lib/api/models/app/integrations/CompanyEnrich";

export const companyEnrichConnectionQuery = (id: string) => queryOptions({
    queryKey: ["integrations", "connection", id], queryFn: () => getConnection(id), staleTime: 10_000,
});

export function useCompanyEnrichConnection(id: string) {
    return useQuery({ ...companyEnrichConnectionQuery(id), enabled: !!id });
}

export function useCompanyEnrichQuery(id: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: CompanyEnrichQuery) => queryCompanyEnrich(id, input), retry: false,
        onSettled: () => { void qc.invalidateQueries({ queryKey: ["integrations", "connection", id] }); },
    });
}

export function useCompanyEnrichContact(id: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: CompanyEnrichContactInput) => enrichCompanyEnrichContact(id, input),
        retry: false,
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ["contacts"] }); },
        onSettled: () => { void qc.invalidateQueries({ queryKey: ["integrations", "connection", id] }); },
    });
}
