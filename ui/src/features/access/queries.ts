import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { accessKeys, listGrants, listGroups, listUsers } from "./api";

export const grantsQueryOptions = queryOptions({
  queryKey: accessKeys.grants,
  queryFn: listGrants,
});

export const usersQueryOptions = queryOptions({
  queryKey: accessKeys.users,
  queryFn: listUsers,
});

export const groupsQueryOptions = queryOptions({
  queryKey: accessKeys.groups,
  queryFn: listGroups,
});

export function useGrants() {
  return useSuspenseQuery(grantsQueryOptions).data;
}
