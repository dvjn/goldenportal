import { createFileRoute, Link } from "@tanstack/react-router";
import { Boxes, Plus } from "lucide-react";
import { EmptyValue } from "@/components/catalog/property-value";
import { DocTitle } from "@/components/config-docs/doc-title";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
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
      <DocTitle
        to="/blueprints/$identifier"
        icon={row.original.icon}
        color={row.original.color}
        title={row.original.title}
        identifier={row.original.identifier}
      />
    ),
  }),
  column.accessor((blueprint) => blueprint.publishedVersion, {
    id: "version",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Version" />,
    enableGlobalFilter: false,
    cell: ({ getValue }) => (getValue() > 0 ? getValue() : <EmptyValue title="Not published" />),
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
      <PageHeader title="Blueprints" />
      <DataTable
        data={blueprints}
        columns={columns}
        getRowId={(blueprint) => blueprint.identifier}
        search={search}
        searchLabel="Search blueprints"
        actions={<NewBlueprintButton />}
        empty={
          <EmptyState
            icon={<Boxes />}
            title="No blueprints yet"
            description="A blueprint defines a kind of entity, such as a service or a team."
          >
            <NewBlueprintButton />
          </EmptyState>
        }
      />
    </div>
  );
}
