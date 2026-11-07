import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plus, Zap } from "lucide-react";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
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
import { type ActionSummary, actions } from "@/features/actions";

export const Route = createFileRoute("/_app/actions/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(actions.queries.list),
  component: ActionsPage,
});

const doc = docColumns<ActionSummary>("Action", "/actions/$identifier");
const column = dataTableColumns<ActionSummary>();

const count = (id: "inputs" | "steps", title: string) =>
  column.accessor((action) => action.stats[id] ?? 0, {
    id,
    header: ({ column }) => <DataTableColumnHeader column={column} title={title} />,
    enableGlobalFilter: false,
    cell: ({ getValue }) => <span className="tabular-nums">{getValue()}</span>,
  });

const columns = [
  doc.title,
  column.accessor((action) => action.facts?.blueprint ?? "", {
    id: "blueprint",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Runs On" />,
    cell: ({ getValue }) => getValue() && <BlueprintLabel blueprint={getValue()} />,
  }),
  count("inputs", "Inputs"),
  count("steps", "Steps"),
  doc.status,
  doc.updated,
];

function NewActionButton() {
  return (
    <Button asChild>
      <Link to="/actions/new">
        <Plus />
        New Action
      </Link>
    </Button>
  );
}

function ActionsPage() {
  const { data: list } = useSuspenseQuery(actions.queries.list);
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Actions"
        icon={<PageIcon page="actions" />}
        actions={<NewActionButton />}
      />
      <DataTable
        noun="actions"
        data={list as ActionSummary[]}
        columns={columns}
        getRowId={(action) => action.identifier}
        search={search}
        searchLabel="Search actions"
        empty={
          <EmptyState
            icon={<Zap />}
            title="No actions yet"
            description="An action does something to one entity at a time, such as restarting a service."
          >
            <NewActionButton />
          </EmptyState>
        }
      />
    </div>
  );
}
