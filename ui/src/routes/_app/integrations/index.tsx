import { createFileRoute, Link } from "@tanstack/react-router";
import { Plug, Plus } from "lucide-react";
import { StatusBadges } from "@/components/config-docs/status-badges";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { type IntegrationSummary, integrations, useIntegrations } from "@/features/integrations";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/integrations/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(integrations.queries.list),
  component: IntegrationsPage,
});

const column = dataTableColumns<IntegrationSummary>();

const columns = [
  // The identifier is part of the value so search finds integrations by either.
  column.accessor((integration) => `${integration.title} ${integration.identifier}`, {
    id: "title",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Integration" />,
    cell: ({ row }) => (
      <Link
        to="/integrations/$identifier"
        params={{ identifier: row.original.identifier }}
        className="grid hover:underline"
      >
        <span className="font-medium">{row.original.title}</span>
        <code className="font-mono text-xs text-muted-foreground">{row.original.identifier}</code>
      </Link>
    ),
  }),
  column.accessor((integration) => integration.publishedVersion, {
    id: "status",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Status" />,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusBadges doc={row.original} />,
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
  const list = useIntegrations();
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Integrations"
        description="Where entities come from. Each entity is written by exactly one integration."
      />
      <DataTable
        data={list}
        columns={columns}
        getRowId={(integration) => integration.identifier}
        search={search}
        searchLabel="Search integrations"
        actions={<NewIntegrationButton />}
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Plug />
              </EmptyMedia>
              <EmptyTitle>No integrations yet</EmptyTitle>
              <EmptyDescription>
                An integration writes entities, from data pushed to the portal.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <NewIntegrationButton />
            </EmptyContent>
          </Empty>
        }
      />
    </div>
  );
}
