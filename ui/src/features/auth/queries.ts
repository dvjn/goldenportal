import { type QueryClient, queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { authKeys, getMe, hasRole, type Role } from "./api";

export const meQueryOptions = queryOptions({
  queryKey: authKeys.me,
  queryFn: getMe,
  staleTime: Infinity,
  retry: false,
});

export function useMe() {
  return useSuspenseQuery(meQueryOptions).data;
}

// ForbiddenError is a page the signed-in user's role can't open. It shows
// in place of the page, like the server's 403s, rather than redirecting.
export class ForbiddenError extends Error {
  readonly role: Role;

  constructor(role: Role) {
    super(`This page needs the ${role} role`);
    this.role = role;
  }
}

// requireRole stops a route loading for anyone without role, before its
// loaders ask the server for what they couldn't read anyway.
export async function requireRole(queryClient: QueryClient, role: Role) {
  const me = await queryClient.ensureQueryData(meQueryOptions);
  if (!hasRole(me.role, role)) throw new ForbiddenError(role);
}

// safeRedirect keeps post-sign-in redirects on this site.
export function safeRedirect(target: string | undefined) {
  return target?.startsWith("/") && !target.startsWith("//") && !target.startsWith("/\\")
    ? target
    : "/";
}
