import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ActionOverview } from "@/components/actions/action-overview";
import { type RunsSearch, RunsTable, validateRunsSearch } from "@/components/actions/runs-table";
import { DocPage, type DocTab, validateDocSearch } from "@/components/config-docs/doc-page";
import { PageError } from "@/components/page-error";
import { actions } from "@/features/actions";

function validateActionSearch(search: Record<string, unknown>): RunsSearch & { tab?: DocTab } {
  return { ...validateDocSearch(search, ["runs"]), ...validateRunsSearch(search) };
}

export const Route = createFileRoute("/_app/actions/$identifier")({
  validateSearch: validateActionSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(actions.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(actions.queries.versions(params.identifier)),
    ]),
  component: ActionPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/actions">Back to Actions</Link>} />
  ),
});

function ActionPage() {
  const { identifier } = Route.useParams();
  const { tab, ...runsSearch } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: action } = useSuspenseQuery(actions.queries.detail(identifier));

  return (
    <DocPage
      docs={actions}
      doc={action}
      parent={<Link to="/actions">Actions</Link>}
      noun="Action"
      deleteDescription="This deletes the action, its versions and draft, and every run of it. Entities it changed stay as they are."
      onDeleted={() => navigate({ to: "/actions" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      overview={(spec, doc) => <ActionOverview spec={spec} action={doc} />}
      extraTabs={[
        {
          value: "runs",
          title: "Runs",
          content: (
            <RunsTable
              scope={{ action: identifier }}
              search={runsSearch}
              empty={{
                title: "Nothing has run yet",
                description: "Runs show up here once someone runs it from an entity's page.",
              }}
            />
          ),
        },
      ]}
    />
  );
}
