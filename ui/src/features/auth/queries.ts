import { queryOptions, useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { navigateAway } from "@/lib/navigation";
import { authKeys, getMe, logout } from "./api";

export const meQueryOptions = queryOptions({
  queryKey: authKeys.me,
  queryFn: getMe,
  staleTime: Infinity,
});

export function useMe() {
  return useSuspenseQuery(meQueryOptions).data;
}

export function useLogout() {
  return useMutation({
    mutationFn: logout,
    onSuccess: ({ redirect_to }) => navigateAway(redirect_to),
  });
}
