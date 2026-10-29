import { queryOptions, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { type AuditQuery, auditKeys, auditQuery, getAuditFacets, getAuditPage } from "./api";

export const auditPageQueryOptions = (query: number | AuditQuery) =>
  queryOptions({
    queryKey: auditKeys.page(auditQuery(query)),
    queryFn: () => getAuditPage(query),
  });

export const auditFacetsQueryOptions = queryOptions({
  queryKey: auditKeys.facets,
  queryFn: getAuditFacets,
});

export function useAuditPage(query: number | AuditQuery) {
  return useSuspenseQuery(auditPageQueryOptions(query)).data;
}

export function useAuditFacets() {
  return useQuery(auditFacetsQueryOptions).data;
}
