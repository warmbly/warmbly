import type { QueryClient, UseMutationOptions } from "@tanstack/react-query";

// Runs a mutation from its options without a mounted useMutation, so a list can
// share one set of handlers; the mutation still sits in the cache under its key.
export default function runMutation<TData, TError, TVariables, TContext>(
    queryClient: QueryClient,
    options: UseMutationOptions<TData, TError, TVariables, TContext>,
    variables: TVariables,
): Promise<TData> {
    return queryClient.getMutationCache().build(queryClient, options).execute(variables);
}
