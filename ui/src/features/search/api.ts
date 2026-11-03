import { queryOptions } from "@tanstack/react-query";
import { type ApiDataResponse, api } from "@/lib/api";

// SearchKind is what a search result is, which says where it opens.
export type SearchKind =
  | "blueprint"
  | "entity"
  | "dashboard"
  | "scorecard"
  | "integration"
  | "action"
  | "workflow";

// SearchResult is something a search found, as the server shows it: an
// entity has its blueprint, and detail, that blueprint's title.
export type SearchResult = {
  kind: SearchKind;
  identifier: string;
  title: string;
  detail?: string;
  blueprint?: string;
  icon?: string;
  color?: string;
};

// search finds what the signed-in user can open across the portal by
// title or identifier, in one list, closest first. Empty text gives what
// to start from.
export async function search(q: string): Promise<SearchResult[]> {
  return (await api.get("search", { searchParams: { q } }).json<ApiDataResponse<SearchResult[]>>())
    .data;
}

export const searchQueryOptions = (q: string) =>
  queryOptions({ queryKey: ["search", q] as const, queryFn: () => search(q) });
