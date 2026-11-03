import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Stamp } from "lucide-react";
import { Requester } from "@/components/actions/runs-table";
import { EntityLink } from "@/components/catalog/entity-link";
import { DataTable, dataTableColumns, validateTableSearch } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import { approvalDetail, approvalsQueryOptions, type Run } from "@/features/actions";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/approvals")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(approvalsQueryOptions),
  component: ApprovalsPage,
});

// waitingStep is the approval step a run waits at.
function waitingStep(run: Run) {
  return run.steps?.find((step) => step.status === "waiting");
}

const column = dataTableColumns<Run>();

const columns = [
  column.accessor((run) => `${run.title} ${approvalMessage(run)}`, {
    id: "run",
    header: "Run",
    cell: ({ row }) => (
      <div className="grid max-w-96 gap-0.5">
        <Link
          to="/runs/$id"
          params={{ id: row.original.id }}
          className="truncate font-medium hover:underline"
        >
          {row.original.title}
        </Link>
        {approvalMessage(row.original) && (
          <span className="truncate text-xs text-muted-foreground">
            {approvalMessage(row.original)}
          </span>
        )}
      </div>
    ),
  }),
  column.accessor((run) => `${run.entity.title} ${run.entity.identifier}`, {
    id: "entity",
    header: "Entity",
    cell: ({ row }) => (
      <EntityLink
        blueprint={row.original.entity.blueprint}
        identifier={row.original.entity.identifier}
      >
        {row.original.entity.title || row.original.entity.identifier}
      </EntityLink>
    ),
  }),
  column.accessor((run) => `${run.requester.name} ${run.requester.email}`, {
    id: "requester",
    header: "Requested By",
    cell: ({ row }) => <Requester run={row.original} />,
  }),
  column.display({
    id: "approvals",
    header: () => <span className="block text-right">Approvals</span>,
    cell: ({ row }) => {
      const step = waitingStep(row.original);
      const detail = step && approvalDetail(step);
      const approved = step?.decisions?.filter((d) => d.decision === "approve").length ?? 0;
      return (
        <span className="block text-right tabular-nums">
          {approved}
          <span className="text-muted-foreground"> / {detail?.require ?? 1}</span>
        </span>
      );
    },
  }),
  column.display({
    id: "waiting",
    header: "Waiting Since",
    cell: ({ row }) => <When at={waitingStep(row.original)?.startedAt ?? row.original.createdAt} />,
  }),
  column.display({
    id: "due",
    header: "Due",
    cell: ({ row }) => <When at={waitingStep(row.original)?.dueAt} />,
  }),
];

function approvalMessage(run: Run) {
  const step = waitingStep(run);
  return (step && approvalDetail(step)?.message) ?? "";
}

function When({ at }: Readonly<{ at?: string | null }>) {
  if (!at) return null;
  return (
    <span className="whitespace-nowrap text-muted-foreground" title={formatAbsolute(at)}>
      {formatRelative(at)}
    </span>
  );
}

// ApprovalsPage lists the runs waiting on the signed-in user's approval,
// oldest first, each opening the run to review and decide.
function ApprovalsPage() {
  const { data: runs = [] } = useQuery(approvalsQueryOptions);
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Approvals" icon={<PageIcon page="approvals" />} />
      <DataTable
        data={runs}
        columns={columns}
        getRowId={(run) => run.id}
        search={search}
        searchLabel="Search approvals"
        empty={
          <EmptyState
            icon={<Stamp />}
            title="Nothing waits on you"
            description="Runs that need your approval show up here."
          />
        }
      />
    </div>
  );
}
