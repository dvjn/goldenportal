import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plug, Plus } from "lucide-react";
import { EmptyValue } from "@/components/catalog/property-value";
import { docColumns } from "@/components/config-docs/doc-columns";
import { RunStatus } from "@/components/connections/run-status";
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
import {
  type ConnectionSummary,
  connections,
  type Run,
  statusQueryOptions,
  useConnections,
} from "@/features/connections";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/connections/")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(connections.queries.list),
  component: ConnectionsPage,
});

// Row is a connection with its latest run, once it's published and has run.
type Row = ConnectionSummary & { lastRun: Run | null };

const column = dataTableColumns<Row>();

const doc = docColumns<Row>("Connection", "/connections/$identifier");

const columns = [
  doc.title,
  doc.status,
  column.accessor((connection) => connection.lastRun?.startedAt ?? "", {
    id: "lastRun",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Last Run" />,
    enableGlobalFilter: false,
    cell: ({ row }) => {
      const run = row.original.lastRun;
      if (!run) return <EmptyValue title="Never run" />;
      return (
        <span className="flex items-center gap-1.5" title={formatAbsolute(run.startedAt)}>
          <RunStatus run={run} />
          <span className="text-muted-foreground">{formatRelative(run.startedAt)}</span>
        </span>
      );
    },
  }),
  doc.updated,
];

function NewConnectionButton() {
  return (
    <Button asChild>
      <Link to="/connections/new">
        <Plus />
        New Connection
      </Link>
    </Button>
  );
}

function ConnectionsPage() {
  const connectionList = useConnections();
  const { data: status } = useQuery(statusQueryOptions);
  const list: Row[] = connectionList.map((connection) => ({
    ...connection,
    lastRun: status?.find((s) => s.connection === connection.identifier)?.lastRun ?? null,
  }));
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Connections"
        icon={<PageIcon page="connections" />}
        actions={<NewConnectionButton />}
      />
      <DataTable
        noun="connections"
        data={list}
        columns={columns}
        getRowId={(connection) => connection.identifier}
        search={search}
        searchLabel="Search connections"
        empty={
          <EmptyState
            icon={<Plug />}
            title="No connections yet"
            description="A connection writes entities from data it pulls or that is pushed to it."
          >
            <NewConnectionButton />
          </EmptyState>
        }
      />
    </div>
  );
}
