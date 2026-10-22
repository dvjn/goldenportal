import { createFileRoute, Link } from "@tanstack/react-router";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  connectorLabels,
  type IntegrationSummary,
  integrationsQueryOptions,
  RunStatusBadge,
  useIntegrations,
} from "@/features/integrations";
import { formatDateTime, formatRelative } from "@/lib/time";

export const Route = createFileRoute("/_authenticated/integrations/")({
  loader: ({ context }) => context.queryClient.ensureQueryData(integrationsQueryOptions),
  component: IntegrationsPage,
});

function IntegrationsPage() {
  const integrations = useIntegrations();

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">Integrations</h1>
        <p className="text-muted-foreground text-sm">
          Each integration owns the entities it writes to the catalog. Define integrations in the
          definitions editor.
        </p>
      </header>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Connector</TableHead>
            <TableHead className="text-right">Entities</TableHead>
            <TableHead>Last run</TableHead>
            <TableHead>Next run</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {integrations.map((integration) => (
            <TableRow key={integration.identifier}>
              <TableCell>
                <Link
                  to="/integrations/$integration"
                  params={{ integration: integration.identifier }}
                  className="font-medium underline-offset-4 hover:underline"
                >
                  {integration.title}
                </Link>
              </TableCell>
              <TableCell>
                {connectorLabels[integration.connector] ?? integration.connector}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {integration.entity_count.toLocaleString("en")}
              </TableCell>
              <TableCell>
                <LastRun integration={integration} />
              </TableCell>
              <TableCell>
                <NextRun integration={integration} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </>
  );
}

function LastRun({ integration }: { integration: IntegrationSummary }) {
  const run = integration.last_run;
  if (!run) return <span className="text-muted-foreground">No runs yet</span>;

  return (
    <span className="flex items-center gap-2">
      <RunStatusBadge status={run.status} />
      <span title={formatDateTime(run.started_at)}>{formatRelative(run.started_at)}</span>
    </span>
  );
}

function NextRun({ integration }: { integration: IntegrationSummary }) {
  if (integration.next_run_at) {
    return (
      <span title={formatDateTime(integration.next_run_at)}>
        {formatRelative(integration.next_run_at)}
      </span>
    );
  }
  return (
    <span className="text-muted-foreground">
      {unscheduled[integration.connector] ?? "Not scheduled"}
    </span>
  );
}

const unscheduled: Record<string, string> = {
  manual: "When people edit entities",
  push: "When the source pushes",
};
