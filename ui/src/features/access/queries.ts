import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { authKeys } from "@/features/auth";
import { settingsKeys } from "@/features/settings";
import {
  accessKeys,
  getGrants,
  getSubjects,
  getUserCheck,
  removeGrant,
  revokeSessions,
  setGrant,
} from "./api";

export const grantsQueryOptions = queryOptions({
  queryKey: accessKeys.grants,
  queryFn: getGrants,
});

export const subjectsQueryOptions = queryOptions({
  queryKey: accessKeys.subjects,
  queryFn: getSubjects,
});

export const userCheckQueryOptions = (id: string) =>
  queryOptions({
    queryKey: accessKeys.user(id),
    queryFn: () => getUserCheck(id),
  });

export function useGrants() {
  return useSuspenseQuery(grantsQueryOptions).data;
}

export function useSubjects() {
  return useSuspenseQuery(subjectsQueryOptions).data;
}

export function useUserCheck(id: string) {
  return useSuspenseQuery(userCheckQueryOptions(id)).data;
}

// A grant can change the bootstrap banner and the signed-in user's own role.
function useInvalidateAccess() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: accessKeys.all }),
      queryClient.invalidateQueries({ queryKey: settingsKeys.settings, exact: true }),
      queryClient.invalidateQueries({ queryKey: authKeys.me }),
    ]);
}

export function useSetGrant() {
  const invalidate = useInvalidateAccess();
  return useMutation({ mutationFn: setGrant, onSuccess: invalidate });
}

export function useRemoveGrant() {
  const invalidate = useInvalidateAccess();
  return useMutation({ mutationFn: removeGrant, onSuccess: invalidate });
}

export function useRevokeSessions() {
  return useMutation({ mutationFn: revokeSessions });
}
