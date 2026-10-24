import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { authKeys, getMe } from "./api";

export const meQueryOptions = queryOptions({
  queryKey: authKeys.me,
  queryFn: getMe,
  staleTime: Infinity,
  retry: false,
});

export function useMe() {
  return useSuspenseQuery(meQueryOptions).data;
}

// safeRedirect keeps post-sign-in redirects on this site.
export function safeRedirect(target: string | undefined) {
  return target?.startsWith("/") && !target.startsWith("//") && !target.startsWith("/\\")
    ? target
    : "/";
}
