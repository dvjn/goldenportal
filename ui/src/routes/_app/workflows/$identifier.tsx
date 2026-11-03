import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ActionOverview } from "@/components/actions/action-overview";
import { type RunsSearch, RunsTable, validateRunsSearch } from "@/components/actions/runs-table";
import { DocPage, type DocTab, validateDocSearch } from "@/components/config-docs/doc-page";
import { PageError } from "@/components/page-error";
import { Badge } from "@/components/ui/badge";
import { workflows } from "@/features/actions";

function validateWorkflowSearch(search: Record<string, unknown>): RunsSearch & { tab?: DocTab } {
  return { ...validateDocSearch(search, ["runs"]), ...validateRunsSearch(search) };
}

export const Route = createFileRoute("/_app/workflows/$identifier")({
  validateSearch: validateWorkflowSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(workflows.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(workflows.queries.versions(params.identifier)),
    ]),
  component: WorkflowPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/workflows">Back to Workflows</Link>} />
  ),
});

function WorkflowPage() {
  const { identifier } = Route.useParams();
  const { tab, ...runsSearch } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: doc } = useSuspenseQuery(workflows.queries.detail(identifier));
  const destructive = (doc.published?.spec ?? doc.draft?.spec)?.destructive;

  return (
    <DocPage
      docs={workflows}
      doc={doc}
      parent={<Link to="/workflows">Workflows</Link>}
      noun="Workflow"
      deleteDescription="This deletes the workflow, its versions and draft, and every run of it. What its runs changed stays as it is."
      onDeleted={() => navigate({ to: "/workflows" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      badges={destructive && <Badge variant="destructive">Destructive</Badge>}
      overview={(spec, d) => <ActionOverview spec={spec} action={d} />}
      extraTabs={[
        {
          value: "runs",
          title: "Runs",
          content: (
            <RunsTable
              scope={{ workflow: identifier }}
              search={runsSearch}
              hide={["entity"]}
              empty={{
                title: "Nothing has run yet",
                description: "Runs show up here once someone runs it from self-service.",
              }}
            />
          ),
        },
      ]}
    />
  );
}
