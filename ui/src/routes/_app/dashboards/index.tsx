import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { EmptyValue } from "@/components/catalog/property-value";
import { docColumns } from "@/components/config-docs/doc-columns";
import { AudienceValue } from "@/components/dashboards/audience-value";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { type DashboardSummary, dashboards } from "@/features/dashboards";

export const Route = createFileRoute("/_app/dashboards/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(dashboards.queries.list),
  component: DashboardsPage,
});

const doc = docColumns<DashboardSummary>("Dashboard", "/dashboards/$identifier");
const column = dataTableColumns<DashboardSummary>();

const columns = [
  doc.title,
  column.accessor((dashboard) => dashboard.facts?.order ?? 0, {
    id: "order",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Order" />,
    enableGlobalFilter: false,
    cell: ({ getValue }) => <span className="tabular-nums">{getValue()}</span>,
  }),
  column.display({
    id: "audience",
    header: () => "Audience",
    cell: ({ row }) => (
      <AudienceValue audience={row.original.facts?.visibility} none={<span>Everyone</span>} />
    ),
  }),
  column.display({
    id: "default",
    header: () => "Default For",
    cell: ({ row }) => (
      <AudienceValue audience={row.original.facts?.defaultFor} none={<EmptyValue />} />
    ),
  }),
  doc.status,
  doc.updated,
];

function NewDashboardButton() {
  return (
    <Button asChild>
      <Link to="/dashboards/new">
        <Plus />
        New Dashboard
      </Link>
    </Button>
  );
}

function DashboardsPage() {
  const { data: list } = useSuspenseQuery(dashboards.queries.list);
  const search = Route.useSearch();
  // Home's tab order leads, so the table reads as Home does.
  const rows = [...(list as DashboardSummary[])].sort(
    (a, b) => (a.facts?.order ?? 0) - (b.facts?.order ?? 0) || a.title.localeCompare(b.title),
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Dashboards" actions={<NewDashboardButton />} />
      <DataTable
        data={rows}
        columns={columns}
        getRowId={(dashboard) => dashboard.identifier}
        search={search}
        searchLabel="Search dashboards"
        empty={
          <EmptyState
            icon={<LayoutDashboard />}
            title="No dashboards yet"
            description="A dashboard is a grid of widgets, shown as a tab on Home, a blueprint's catalog page or its entities' pages."
          >
            <NewDashboardButton />
          </EmptyState>
        }
      />
    </div>
  );
}
