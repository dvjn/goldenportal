import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { DocPage, type DocTab, validateDocSearch } from "@/components/config-docs/doc-page";
import { validateTableSearch } from "@/components/data-table";
import {
  ScorecardOverview,
  type ScorecardSearch,
} from "@/components/scorecards/scorecard-overview";
import { hasRole, useMe } from "@/features/auth";
import { scorecards } from "@/features/scorecards";

// The entities table pages and filters by tier on the server, but doesn't sort.
function validateScorecardSearch(
  search: Record<string, unknown>,
): ScorecardSearch & { tab?: DocTab } {
  const { sort: _sort, ...table } = validateTableSearch(search);
  const tier = typeof search.tier === "string" && search.tier !== "" ? search.tier : undefined;
  return { ...validateDocSearch(search), ...table, ...(tier ? { tier } : {}) };
}

export const Route = createFileRoute("/_app/scorecards/$identifier")({
  validateSearch: validateScorecardSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(scorecards.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(scorecards.queries.versions(params.identifier)),
    ]),
  component: ScorecardPage,
});

function ScorecardPage() {
  const { identifier } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: scorecard } = useSuspenseQuery(scorecards.queries.detail(identifier));
  const builder = hasRole(useMe().role, "builder");
  const tableSearch: ScorecardSearch = search;

  return (
    <DocPage
      docs={scorecards}
      doc={scorecard}
      parent={<Link to="/scorecards">Scorecards</Link>}
      noun="Scorecard"
      deleteDescription="This deletes the scorecard, its versions and draft, and every result it holds. Entities aren't touched."
      onDeleted={() => navigate({ to: "/scorecards" })}
      tab={search.tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      editable={builder}
      overview={(spec) => (
        <ScorecardOverview
          spec={spec}
          search={tableSearch}
          canEvaluate={builder && scorecard.published !== null}
        />
      )}
    />
  );
}
