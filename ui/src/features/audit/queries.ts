import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { type AuditFilters, auditKeys, listAuditEvents } from "./api";

export function auditQueryOptions(filters: AuditFilters) {
  return queryOptions({
    queryKey: auditKeys.page(filters),
    queryFn: () => listAuditEvents(filters),
    // Keep showing the current page while the next one loads.
    placeholderData: keepPreviousData,
  });
}

export function useAuditEvents(filters: AuditFilters) {
  return useQuery(auditQueryOptions(filters));
}
