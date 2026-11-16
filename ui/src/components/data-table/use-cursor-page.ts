import {
  keepPreviousData,
  type QueryKey,
  type UseQueryOptions,
  useQuery,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { type CursorPage, isBadRequest } from "@/lib/api";
import type { TableSearch } from "./table";

// useCursorPage reads the page a table's cursor asks for, and beside it the
// first page, which alone has the total; on the first page they're one
// query. A cursor the server turns down, as an old link's can be, goes
// back to the first page.
export function useCursorPage<TItem, TKey extends QueryKey>(
  options: (cursor?: string) => UseQueryOptions<CursorPage<TItem>, Error, CursorPage<TItem>, TKey>,
  cursor: string | undefined,
) {
  const navigate = useNavigate();
  // A page that failed as its loader read it shows why, rather than asking again.
  const settings = { placeholderData: keepPreviousData, retryOnMount: false };
  const page = useQuery({ ...options(cursor), ...settings });
  const first = useQuery({ ...options(), ...settings });
  const refused = cursor !== undefined && isBadRequest(page.error);
  useEffect(() => {
    if (!refused) return;
    const search = (prev: TableSearch) => ({ ...prev, cursor: undefined });
    void navigate({ to: ".", search: search as never, replace: true, resetScroll: false });
  }, [refused, navigate]);

  return {
    page,
    first,
    total: page.data?.total ?? first.data?.total ?? 0,
    pending: page.isPending || first.isPending,
  };
}
