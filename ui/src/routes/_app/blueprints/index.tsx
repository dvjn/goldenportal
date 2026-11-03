import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Boxes, Plus } from "lucide-react";
import { EmptyValue } from "@/components/catalog/property-value";
import { docColumns } from "@/components/config-docs/doc-columns";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import { Button } from "@/components/ui/button";
import { type BlueprintSummary, blueprints, useBlueprints } from "@/features/blueprints";
import { catalogQueryOptions } from "@/features/catalog";

export const Route = createFileRoute("/_app/blueprints/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(blueprints.queries.list),
  component: BlueprintsPage,
});

// Row is a blueprint with how many entities it has, once it's published.
type Row = BlueprintSummary & { entities?: number };

const doc = docColumns<Row>("Blueprint", "/blueprints/$identifier");
const column = dataTableColumns<Row>();

const columns = [
  doc.title,
  doc.status,
  column.accessor((blueprint) => blueprint.entities ?? -1, {
    id: "entities",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Entities" />,
    enableGlobalFilter: false,
    cell: ({ row }) => row.original.entities ?? <EmptyValue title="Not published" />,
  }),
  doc.updated,
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
  const list = useBlueprints();
  const { data: catalog } = useQuery(catalogQueryOptions);
  const counts = new Map(catalog?.map((entry) => [entry.identifier, entry.entities]));
  const rows: Row[] = list.map((blueprint) => ({
    ...blueprint,
    entities: counts.get(blueprint.identifier),
  }));
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Blueprints"
        icon={<PageIcon page="blueprints" />}
        actions={<NewBlueprintButton />}
      />
      <DataTable
        data={rows}
        columns={columns}
        getRowId={(blueprint) => blueprint.identifier}
        search={search}
        searchLabel="Search blueprints"
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
