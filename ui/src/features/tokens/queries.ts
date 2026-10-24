import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { listTokens, tokenKeys } from "./api";

export const tokensQueryOptions = queryOptions({
  queryKey: tokenKeys.all,
  queryFn: listTokens,
});

export function useTokens() {
  return useSuspenseQuery(tokensQueryOptions).data;
}
