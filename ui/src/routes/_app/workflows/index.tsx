import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plus, Workflow } from "lucide-react";
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
import { type WorkflowSummary, workflows } from "@/features/actions";

export const Route = createFileRoute("/_app/workflows/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(workflows.queries.list),
  component: WorkflowsPage,
});

const doc = docColumns<WorkflowSummary>("Workflow", "/workflows/$identifier");
const column = dataTableColumns<WorkflowSummary>();

const count = (id: "inputs" | "steps", title: string) =>
  column.accessor((item) => item.stats[id] ?? 0, {
    id,
    header: ({ column }) => <DataTableColumnHeader column={column} title={title} />,
    enableGlobalFilter: false,
    cell: ({ getValue }) => <span className="tabular-nums">{getValue()}</span>,
  });

const columns = [
  doc.title,
  column.accessor((item) => item.facts?.group ?? "", {
    id: "group",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Group" />,
  }),
  count("inputs", "Inputs"),
  count("steps", "Steps"),
  doc.status,
  doc.updated,
];

function NewWorkflowButton() {
  return (
    <Button asChild>
      <Link to="/workflows/new">
        <Plus />
        New Workflow
      </Link>
    </Button>
  );
}

function WorkflowsPage() {
  const { data: list } = useSuspenseQuery(workflows.queries.list);
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Workflows"
        icon={<PageIcon page="workflows" />}
        actions={<NewWorkflowButton />}
      />
      <DataTable
        data={list as WorkflowSummary[]}
        columns={columns}
        getRowId={(item) => item.identifier}
        search={search}
        searchLabel="Search workflows"
        empty={
          <EmptyState
            icon={<Workflow />}
            title="No workflows yet"
            description="A workflow runs as self-service without starting from an entity, such as registering a service."
          >
            <NewWorkflowButton />
          </EmptyState>
        }
      />
    </div>
  );
}
