import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plug, Plus } from "lucide-react";
import { EmptyValue } from "@/components/catalog/property-value";
import { DocTitle } from "@/components/config-docs/doc-title";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { RunStatus } from "@/components/integrations/run-status";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  type IntegrationSummary,
  integrations,
  type Run,
  statusQueryOptions,
  useIntegrations,
} from "@/features/integrations";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/integrations/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(integrations.queries.list),
  component: IntegrationsPage,
});

// Row is an integration with its latest run, once it's published and has run.
type Row = IntegrationSummary & { lastRun: Run | null };

const column = dataTableColumns<Row>();

const columns = [
  // The identifier is part of the value so search finds integrations by either.
  column.accessor((integration) => `${integration.title} ${integration.identifier}`, {
    id: "title",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Integration" />,
    cell: ({ row }) => (
      <DocTitle
        to="/integrations/$identifier"
        icon={row.original.icon}
        color={row.original.color}
        title={row.original.title}
        identifier={row.original.identifier}
      />
    ),
  }),
  column.accessor((integration) => integration.publishedVersion, {
    id: "version",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Version" />,
    enableGlobalFilter: false,
    cell: ({ getValue }) => (getValue() > 0 ? getValue() : <EmptyValue title="Not published" />),
  }),
  column.accessor((integration) => integration.lastRun?.startedAt ?? "", {
    id: "lastRun",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Last Run" />,
    enableGlobalFilter: false,
    cell: ({ row }) => {
      const run = row.original.lastRun;
      if (!run) return <span className="text-muted-foreground">Never run</span>;
      return (
        <span className="flex items-center gap-1.5" title={formatAbsolute(run.startedAt)}>
          <RunStatus run={run} />
          <span className="text-muted-foreground">{formatRelative(run.startedAt)}</span>
        </span>
      );
    },
  }),
  column.accessor((integration) => integration.stats.mappings ?? 0, {
    id: "mappings",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Mappings" />,
    enableGlobalFilter: false,
  }),
  column.accessor((integration) => new Date(integration.updatedAt), {
    id: "updated",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Updated" />,
    sortFn: "datetime",
    enableGlobalFilter: false,
    cell: ({ getValue }) => (
      <span className="text-muted-foreground" title={formatAbsolute(getValue())}>
        {formatRelative(getValue())}
      </span>
    ),
  }),
];

function NewIntegrationButton() {
  return (
    <Button asChild>
      <Link to="/integrations/new">
        <Plus />
        New Integration
      </Link>
    </Button>
  );
}

function IntegrationsPage() {
  const integrationList = useIntegrations();
  const { data: status } = useQuery(statusQueryOptions);
  const list: Row[] = integrationList.map((integration) => ({
    ...integration,
    lastRun: status?.find((s) => s.integration === integration.identifier)?.lastRun ?? null,
  }));
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Integrations" />
      <DataTable
        data={list}
        columns={columns}
        getRowId={(integration) => integration.identifier}
        search={search}
        searchLabel="Search integrations"
        actions={<NewIntegrationButton />}
        empty={
          <EmptyState
            icon={<Plug />}
            title="No integrations yet"
            description="An integration writes entities, from data pushed to the portal."
          >
            <NewIntegrationButton />
          </EmptyState>
        }
      />
    </div>
  );
}
