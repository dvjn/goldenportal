import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { actions, runKeys, workflows } from "@/features/actions";
import { blueprints } from "@/features/blueprints";
import { catalogKeys } from "@/features/catalog";
import { connectionKeys, connections } from "@/features/connections";
import { dashboards } from "@/features/dashboards";
import { scorecardKeys, scorecards } from "@/features/scorecards";
import { searchKeys } from "@/features/search";
import { widgetKeys } from "@/features/widgets";

// Scorecards' results show in the catalog, on entities' pages and in
// widgets. Keys are read when called, as scorecards' own hooks import this.
const resultReaders = (): QueryKey[] => [catalogKeys.index, scorecardKeys.entities, widgetKeys.all];

// Everything shown reads what's published: every kind of document, the
// catalog, scorecards' results, widgets, search and self-service.
const publishedReaders = (): QueryKey[] => [
  ...[actions, blueprints, connections, dashboards, scorecards, workflows].map((d) => d.keys.all),
  ...resultReaders(),
  searchKeys.all,
  runKeys.catalog,
  connectionKeys.status,
];

function invalidate(queryClient: QueryClient, keys: QueryKey[]) {
  return Promise.all(keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

// invalidatePublished refreshes what a publish or a delete can change.
export function invalidatePublished(queryClient: QueryClient) {
  return invalidate(queryClient, publishedReaders());
}

// invalidateResults refreshes what a scorecard's evaluation can change.
export function invalidateResults(queryClient: QueryClient, scorecard: string) {
  return invalidate(queryClient, [scorecards.keys.detail(scorecard), ...resultReaders()]);
}
