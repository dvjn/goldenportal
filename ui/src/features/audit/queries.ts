import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { auditKeys, getAuditPage } from "./api";

export const auditPageQueryOptions = (page: number) =>
  queryOptions({
    queryKey: auditKeys.page(page),
    queryFn: () => getAuditPage(page),
  });

export function useAuditPage(page: number) {
  return useSuspenseQuery(auditPageQueryOptions(page)).data;
}
