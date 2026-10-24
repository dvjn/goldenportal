import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { accessKeys, listGroups, listUsers } from "./api";

export const usersQueryOptions = queryOptions({
  queryKey: accessKeys.users,
  queryFn: listUsers,
});

export const groupsQueryOptions = queryOptions({
  queryKey: accessKeys.groups,
  queryFn: listGroups,
});

export function useUsers() {
  return useSuspenseQuery(usersQueryOptions).data;
}

export function useGroups() {
  return useSuspenseQuery(groupsQueryOptions).data;
}
