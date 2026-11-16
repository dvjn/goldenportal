import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { type AuditFilters, auditKeys, listAuditEvents } from "./api";

export function auditQueryOptions(filters: AuditFilters) {
  return queryOptions({
    queryKey: auditKeys.page(filters),
    queryFn: () => listAuditEvents(filters),
    // Keep showing the current page while the next one loads.
    placeholderData: keepPreviousData,
    // Every change writes an entry, so the log is read afresh each visit.
    staleTime: 0,
  });
}

export function useAuditEvents(filters: AuditFilters) {
  return useQuery(auditQueryOptions(filters));
}
