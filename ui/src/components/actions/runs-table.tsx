import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Bot, History } from "lucide-react";
import type { ReactNode } from "react";
import { EntityLink } from "@/components/catalog/entity-link";
import {
  DataTable,
  type DataTableColumn,
  type DataTableFilterField,
  dataTableColumns,
  type TableSearch,
  usePageSize,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import {
  type Run,
  type RunFilters,
  runsQueryOptions,
  type RunStatus as Status,
} from "@/features/actions";
import { formatAbsolute, formatDuration, formatRelative } from "@/lib/format";
import { RunStatus, runStatuses } from "./run-status";

// RunsSearch is a runs table's state in the URL. status is a
// comma-separated list.
export type RunsSearch = TableSearch & { status?: string };

const statusOptions = (Object.keys(runStatuses) as Status[]).map((value) => ({
  value,
  label: runStatuses[value].label,
}));

const statusField: DataTableFilterField = {
  kind: "options",
  id: "status",
  title: "Status",
  useOptions: () => statusOptions,
};

const requesterOptions = [{ value: "me", label: "Me" }];

// requesterField narrows runs to one's own, which is all members ever see.
const requesterField: DataTableFilterField = {
  kind: "options",
  id: "requester",
  title: "Requested By",
  useOptions: () => requesterOptions,
};

// validateRunsSearch reads a runs table's state. The server pages and
// filters runs, newest first, but doesn't sort them.
export function validateRunsSearch(search: Record<string, unknown>): RunsSearch {
  const { sort: _sort, ...table } = validateTableSearch(search);
  const statuses =
    typeof search.status === "string"
      ? search.status.split(",").filter((status) => status in runStatuses)
      : [];
  return { ...table, ...(statuses.length > 0 ? { status: statuses.join(",") } : {}) };
}

const column = dataTableColumns<Run>();

const columns = {
  status: column.display({
    id: "status",
    header: () => <span className="sr-only">Status</span>,
    cell: ({ row }) => <RunStatus status={row.original.status} />,
  }),
  run: column.display({
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
        {row.original.summary && (
          <span className="truncate text-xs text-muted-foreground" title={row.original.summary}>
            {row.original.summary}
          </span>
        )}
      </div>
    ),
  }),
  entity: column.display({
    id: "entity",
    header: "Entity",
    cell: ({ row }) => {
      const { entity } = row.original;
      // A workflow runs on no entity.
      if (!entity.identifier) return null;
      return (
        <EntityLink blueprint={entity.blueprint} identifier={entity.identifier}>
          {entity.title || entity.identifier}
        </EntityLink>
      );
    },
  }),
  requester: column.display({
    id: "requester",
    header: "Requested By",
    cell: ({ row }) => <Requester run={row.original} />,
  }),
  when: column.display({
    id: "when",
    header: "When",
    cell: ({ row }) => (
      <span
        className="whitespace-nowrap text-muted-foreground"
        title={formatAbsolute(row.original.createdAt)}
      >
        {formatRelative(row.original.createdAt)}
      </span>
    ),
  }),
  took: column.display({
    id: "took",
    header: () => <span className="block text-right">Took</span>,
    cell: ({ row }) => (
      <span className="block text-right text-muted-foreground tabular-nums">
        <Took run={row.original} />
      </span>
    ),
  }),
};

// Took says how long a finished run took.
export function Took({ run }: Readonly<{ run: Pick<Run, "startedAt" | "finishedAt"> }>) {
  if (!run.startedAt || !run.finishedAt) return null;
  return <>{formatDuration(run.startedAt, run.finishedAt)}</>;
}

// Requester names who asked for a run, marking runs an agent asked for.
export function Requester({ run }: Readonly<{ run: Run }>) {
  return (
    <span className="inline-flex items-center gap-1.5" title={run.requester.email}>
      {run.requester.name || run.requester.email}
      {run.via === "mcp" && (
        <Bot className="size-3.5 text-muted-foreground" aria-label="Through an agent" />
      )}
    </span>
  );
}

type RunsTableProps = {
  // scope picks the runs: of an action, on an entity, or only one's own.
  scope: Omit<RunFilters, "q" | "statuses" | "page" | "size">;
  search: RunsSearch;
  // hide leaves out what the scope already says, such as the entity on its page.
  hide?: ("entity" | "requester")[];
  // fixedFilters show what the scope narrows to, for removing it.
  fixedFilters?: { title: string; summary: string; onRemove: () => void }[];
  // mine, when set, offers the Requested By filter, for those who see everyone's runs.
  mine?: { value: boolean; onChange: (mine: boolean) => void };
  title?: string;
  empty: { title: string; description: ReactNode };
};

// RunsTable pages through runs of actions, newest first, filtered by status.
export function RunsTable({
  scope,
  search,
  hide = [],
  fixedFilters,
  mine,
  title,
  empty,
}: RunsTableProps) {
  const navigate = useNavigate();
  const statuses = (search.status?.split(",") ?? []) as Status[];
  const page = useQuery({
    ...runsQueryOptions({
      ...scope,
      q: search.q,
      statuses,
      page: search.page,
      size: usePageSize(search),
    }),
    placeholderData: keepPreviousData,
  });

  function filter(patch: Partial<RunsSearch>) {
    void navigate({
      to: ".",
      search: (prev: RunsSearch) => ({ ...prev, ...patch, page: undefined }),
      replace: true,
      resetScroll: false,
    } as never);
  }

  const shown: DataTableColumn<Run>[] = [
    columns.status,
    columns.run,
    ...(hide.includes("entity") ? [] : [columns.entity]),
    ...(hide.includes("requester") ? [] : [columns.requester]),
    columns.when,
    columns.took,
  ];

  return (
    <DataTable
      title={title}
      data={page.data?.runs ?? []}
      rowCount={page.data?.total ?? 0}
      loading={page.isPlaceholderData}
      pending={page.isPending}
      error={page.error}
      onRetry={() => void page.refetch()}
      columns={shown}
      getRowId={(run) => run.id}
      search={search}
      searchLabel="Search runs"
      fixedFilters={fixedFilters}
      onClearFilters={() => {
        filter({ status: undefined });
        mine?.onChange(false);
        for (const fixed of fixedFilters ?? []) fixed.onRemove();
      }}
      filters={{
        fields: mine ? [requesterField, statusField] : [statusField],
        value: {
          ...(mine?.value ? { requester: ["me"] } : {}),
          ...(statuses.length > 0 ? { status: statuses } : {}),
        },
        onChange: (id, value) => {
          const values = Array.isArray(value) ? value : [];
          if (id === "requester") {
            mine?.onChange(values.includes("me"));
            return;
          }
          filter({ status: values.length > 0 ? values.join(",") : undefined });
        },
      }}
      empty={<EmptyState icon={<History />} title={empty.title} description={empty.description} />}
    />
  );
}
