import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { RunControls } from "@/components/actions/run-controls";
import { RunDetails } from "@/components/actions/run-details";
import { RunStatusBadge, runStatuses } from "@/components/actions/run-status";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { EntityLink } from "@/components/catalog/entity-link";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageError } from "@/components/page-error";
import { PageHeader } from "@/components/page-header";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { runDocs, runQueryOptions } from "@/features/actions";

export const Route = createFileRoute("/_app/runs/$id")({
  loader: ({ context, params }) => context.queryClient.ensureQueryData(runQueryOptions(params.id)),
  component: RunPage,
  errorComponent: (props) => <PageError {...props} back={<Link to="/runs">Back to Runs</Link>} />,
});

function RunPage() {
  const { id } = Route.useParams();
  const { data: run } = useSuspenseQuery(runQueryOptions(id));
  const { data: action } = useQuery(runDocs(run.kind).queries.detail(run.action));
  const look = runStatuses[run.status];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={run.title}
        icon={<IconTile icon={action?.icon} color={action?.color} size="lg" />}
        breadcrumbs={
          <PageBreadcrumbs page={run.title}>
            <Link to="/runs">Runs</Link>
          </PageBreadcrumbs>
        }
        badges={<RunStatusBadge status={run.status} />}
        description={
          // A workflow runs on no entity, so there's none to name.
          run.entity.identifier && (
            <EntityLink blueprint={run.entity.blueprint} identifier={run.entity.identifier}>
              {run.entity.title || run.entity.identifier}
            </EntityLink>
          )
        }
        actions={<RunControls run={run} />}
      />
      {run.error && (
        <Alert variant={run.status === "failed" ? "destructive" : "default"}>
          <look.icon />
          <AlertTitle>{look.label}</AlertTitle>
          <AlertDescription>{run.error}</AlertDescription>
        </Alert>
      )}
      <RunDetails run={run} />
    </div>
  );
}
