import deleteCategory from "@/lib/api/client/app/categories/deleteCategory";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteCategory(id: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => deleteCategory(id),
    onSuccess: () => {
      queryClient.setQueryData<User>(["auth", "me"], (oldData) => {
        if (!oldData) return oldData;

        return {
          ...oldData,
          categories: oldData.categories.filter((t) => t.id !== id),
        };
      });
      // The category is also a Unibox conversation label. The DB
      // cascades the unibox_thread_labels rows away, but the cached
      // list rows + scope-rail counts still reference it — refresh
      // them so deleted-label chips/counts don't linger.
      queryClient.invalidateQueries({ queryKey: ["unibox", "search"] });
      queryClient.invalidateQueries({ queryKey: ["unibox", "overview"] });
    },
  });
}
