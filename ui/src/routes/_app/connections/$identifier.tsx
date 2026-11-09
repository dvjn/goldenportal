import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { DocPage, type DocTab, validateDocSearch } from "@/components/config-docs/doc-page";
import { LastRun, SyncNow } from "@/components/connections/connection-header";
import { ConnectionOverview } from "@/components/connections/connection-overview";
import { PushPanel } from "@/components/connections/push-panel";
import {
  RecentRuns,
  type RunsSearch,
  validateRunsSearch,
} from "@/components/connections/recent-runs";
import { PageError } from "@/components/page-error";
import { connections, useConnection } from "@/features/connections";

function validateConnectionSearch(search: Record<string, unknown>): RunsSearch & { tab?: DocTab } {
  return { ...validateDocSearch(search, ["runs", "push"]), ...validateRunsSearch(search) };
}

export const Route = createFileRoute("/_app/connections/$identifier")({
  validateSearch: validateConnectionSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(connections.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(connections.queries.versions(params.identifier)),
    ]),
  component: ConnectionPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/connections">Back to Connections</Link>} />
  ),
});

function ConnectionPage() {
  const { identifier } = Route.useParams();
  const { tab, ...runsSearch } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const connection = useConnection(identifier);
  // The connector config decides how data arrives: pulled on a schedule,
  // which Sync Now runs early once published, pushed to the endpoint, or
  // both. Tokens can be made while only the draft takes pushes.
  const pulls = connection.published?.spec.pulls;
  // Events come to a webhook the connector sets up, so there's nothing to push by hand.
  const spec = connection.published?.spec ?? connection.draft?.spec;
  const receives =
    (connection.published?.spec.receives || connection.draft?.spec.receives) && !spec?.events;

  return (
    <DocPage
      docs={connections}
      doc={connection}
      parent={<Link to="/connections">Connections</Link>}
      noun="Connection"
      deleteDescription="This deletes the connection, its versions, draft and tokens, and every entity it wrote. It can't be undone."
      onDeleted={() => navigate({ to: "/connections" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      badges={connection.published && <LastRun identifier={identifier} />}
      actions={pulls && <SyncNow identifier={identifier} />}
      overview={(spec) => <ConnectionOverview spec={spec} />}
      extraTabs={[
        {
          value: "runs",
          title: "Runs",
          content: <RecentRuns identifier={identifier} search={runsSearch} />,
        },
        ...(receives
          ? [{ value: "push", title: "Push", content: <PushPanel identifier={identifier} /> }]
          : []),
      ]}
    />
  );
}
