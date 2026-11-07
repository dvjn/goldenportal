import { queryOptions } from "@tanstack/react-query";
import {
  getRun,
  getSelfServiceCatalog,
  listApprovals,
  listAvailable,
  listRowActions,
  listRuns,
  type RunFilters,
  type RunStatus,
  runKeys,
} from "./api";

// pollFor is how often a run is read again while it isn't finished. One
// waiting for approval changes only when someone decides, so it's read
// less often.
function pollFor(status: RunStatus) {
  if (status === "queued" || status === "running") return 2000;
  if (status === "waiting") return 10000;
  return false;
}

const soonest = (polls: (number | false)[]) =>
  polls.reduce<number | false>(
    (a, b) => (a === false ? b : b === false ? a : Math.min(a, b)),
    false,
  );

// approvalsPoll keeps the count of runs waiting on the signed-in user fresh.
const approvalsPoll = 30000;

export const availableQueryOptions = (blueprint: string, entity: string) =>
  queryOptions({
    queryKey: runKeys.available(blueprint, entity),
    queryFn: () => listAvailable(blueprint, entity),
  });

// A page with runs under way keeps reading, so their status keeps up.
export const runsQueryOptions = (filters: RunFilters) =>
  queryOptions({
    queryKey: runKeys.list(filters),
    queryFn: () => listRuns(filters),
    refetchInterval: (query) =>
      soonest((query.state.data?.items ?? []).map((run) => pollFor(run.status))),
  });

export const runQueryOptions = (id: string) =>
  queryOptions({
    queryKey: runKeys.detail(id),
    queryFn: () => getRun(id),
    refetchInterval: (query) => (query.state.data ? pollFor(query.state.data.status) : false),
  });

export const rowActionsQueryOptions = (blueprint: string, entities: string[]) =>
  queryOptions({
    queryKey: runKeys.rows(blueprint, entities),
    queryFn: () => listRowActions(blueprint, entities),
    enabled: entities.length > 0,
  });

export const selfServiceCatalogQueryOptions = queryOptions({
  queryKey: runKeys.catalog,
  queryFn: getSelfServiceCatalog,
});

export const approvalsQueryOptions = queryOptions({
  queryKey: runKeys.approvals,
  queryFn: listApprovals,
  refetchInterval: approvalsPoll,
});
