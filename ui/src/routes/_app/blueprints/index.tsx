import { createFileRoute, Link } from "@tanstack/react-router";
import { Boxes, Plus } from "lucide-react";
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
import { type BlueprintSummary, blueprints, useBlueprints } from "@/features/blueprints";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/blueprints/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(blueprints.queries.list),
  component: BlueprintsPage,
});

const column = dataTableColumns<BlueprintSummary>();

const columns = [
  // The identifier is part of the value so search finds blueprints by either.
  column.accessor((blueprint) => `${blueprint.title} ${blueprint.identifier}`, {
    id: "title",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Blueprint" />,
    cell: ({ row }) => (
      <Link
        to="/blueprints/$identifier"
        params={{ identifier: row.original.identifier }}
        className="grid hover:underline"
      >
        <span className="font-medium">{row.original.title}</span>
        <code className="font-mono text-xs text-muted-foreground">{row.original.identifier}</code>
      </Link>
    ),
  }),
  column.accessor((blueprint) => blueprint.publishedVersion, {
    id: "status",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Status" />,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusBadges doc={row.original} />,
  }),
  column.accessor((blueprint) => blueprint.stats.properties ?? 0, {
    id: "properties",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Properties" />,
    enableGlobalFilter: false,
  }),
  column.accessor((blueprint) => blueprint.stats.relations ?? 0, {
    id: "relations",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Relations" />,
    enableGlobalFilter: false,
  }),
  column.accessor((blueprint) => new Date(blueprint.updatedAt), {
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

function NewBlueprintButton() {
  return (
    <Button asChild>
      <Link to="/blueprints/new">
        <Plus />
        New Blueprint
      </Link>
    </Button>
  );
}

function BlueprintsPage() {
  const blueprints = useBlueprints();
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Blueprints"
        description="The kinds of entities in the catalog, and the properties and relations each one has."
      />
      <DataTable
        data={blueprints}
        columns={columns}
        getRowId={(blueprint) => blueprint.identifier}
        search={search}
        searchLabel="Search blueprints"
        actions={<NewBlueprintButton />}
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Boxes />
              </EmptyMedia>
              <EmptyTitle>No blueprints yet</EmptyTitle>
              <EmptyDescription>
                A blueprint defines a kind of entity, such as a service or a team.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <NewBlueprintButton />
            </EmptyContent>
          </Empty>
        }
      />
    </div>
  );
}
