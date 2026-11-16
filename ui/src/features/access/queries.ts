import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { accessKeys, listGrants, searchSubjects } from "./api";

export const grantsQueryOptions = queryOptions({
  queryKey: accessKeys.grants,
  queryFn: listGrants,
});

export const subjectsQueryOptions = (q: string) =>
  queryOptions({
    queryKey: [...accessKeys.subjects, q] as const,
    queryFn: () => searchSubjects(q),
  });

export function useGrants() {
  return useSuspenseQuery(grantsQueryOptions).data;
}
