import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { CircleAlert, History } from "lucide-react";
import {
  DataTable,
  type DataTableFilterField,
  dataTableColumns,
  type TableSearch,
  usePageSize,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { IconButton } from "@/components/icon-button";
import { RunStatus, runOutcomes } from "@/components/integrations/run-status";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  type Run,
  type RunMode,
  type RunOutcome,
  type RunTrigger,
  runsQueryOptions,
} from "@/features/integrations";
import { formatAbsolute, formatDuration, formatRelative } from "@/lib/format";

// RunsSearch is the runs table's state in the URL. trigger and outcome are
// comma-separated lists.
export type RunsSearch = TableSearch & { trigger?: string; outcome?: string };

const triggerOptions: { value: RunTrigger; label: string }[] = [
  { value: "schedule", label: "Schedule" },
  { value: "manual", label: "Sync Now" },
  { value: "push", label: "Push" },
  { value: "action", label: "Action" },
];

const outcomeOptions = (Object.keys(runOutcomes) as RunOutcome[]).map((value) => ({
  value,
  label: runOutcomes[value].label,
}));

const runFilterFields: DataTableFilterField[] = [
  { kind: "options", id: "outcome", title: "Outcome", useOptions: () => outcomeOptions },
  { kind: "options", id: "trigger", title: "Trigger", useOptions: () => triggerOptions },
];

function listFrom<T extends string>(value: unknown, options: { value: T }[]) {
  const allowed = options.map((option) => option.value);
  const list =
    typeof value === "string"
      ? value.split(",").filter((v): v is T => allowed.includes(v as T))
      : [];
  return list.length > 0 ? list.join(",") : undefined;
}

// validateRunsSearch reads the runs table's state. The server pages and
// filters runs, newest first, but doesn't sort them.
export function validateRunsSearch(search: Record<string, unknown>): RunsSearch {
  const { sort: _sort, ...table } = validateTableSearch(search);
  const trigger = listFrom(search.trigger, triggerOptions);
  const outcome = listFrom(search.outcome, outcomeOptions);
  return { ...table, ...(trigger ? { trigger } : {}), ...(outcome ? { outcome } : {}) };
}

const counts = [
  { key: "received", label: "Received" },
  { key: "created", label: "Created" },
  { key: "updated", label: "Updated" },
  { key: "unchanged", label: "Unchanged" },
  { key: "deleted", label: "Deleted" },
  { key: "failed", label: "Failed" },
] as const;

// triggerLabel says what started a run: a push names its token, and an action
// its title.
function triggerLabel(run: Run) {
  if (run.trigger === "schedule") return "Schedule";
  if (run.trigger === "manual") return "Sync Now";
  if (run.trigger === "action") return run.action ? `Action · ${run.action}` : "Action";
  return run.token ? `Push · ${run.token}` : "Push";
}

// A complete run deletes what neither it nor the complete run before it saw; an incremental one
// only what it was told was deleted.
const modeLabels: Record<RunMode, string> = {
  complete: "Complete",
  incremental: "Incremental",
};

const column = dataTableColumns<Run>();

const columns = [
  column.display({
    id: "status",
    header: () => <span className="sr-only">Status</span>,
    cell: ({ row }) => <RunStatus run={row.original} />,
  }),
  column.accessor("startedAt", {
    header: "When",
    enableSorting: false,
    cell: ({ getValue }) => (
      <span className="whitespace-nowrap" title={formatAbsolute(getValue())}>
        {formatRelative(getValue())}
      </span>
    ),
  }),
  column.display({
    id: "trigger",
    header: "Trigger",
    cell: ({ row }) => (
      <span
        className={row.original.error ? "font-medium text-destructive" : "text-muted-foreground"}
      >
        {row.original.actionRun ? (
          <Link to="/runs/$id" params={{ id: row.original.actionRun }} className="hover:underline">
            {triggerLabel(row.original)}
          </Link>
        ) : (
          triggerLabel(row.original)
        )}
      </span>
    ),
  }),
  column.accessor("mode", {
    header: "Mode",
    enableSorting: false,
    cell: ({ getValue }) => <span className="text-muted-foreground">{modeLabels[getValue()]}</span>,
  }),
  column.display({
    id: "took",
    header: () => <span className="block text-right">Took</span>,
    cell: ({ row }) => (
      <span className="block text-right text-muted-foreground tabular-nums">
        {row.original.finishedAt && formatDuration(row.original.startedAt, row.original.finishedAt)}
      </span>
    ),
  }),
  ...counts.map((count) =>
    column.display({
      id: count.key,
      header: () => <span className="block text-right">{count.label}</span>,
      cell: ({ row }) => (
        <span
          className={
            count.key === "failed" && row.original.failed > 0
              ? "block text-right font-medium text-destructive tabular-nums"
              : "block text-right text-muted-foreground tabular-nums"
          }
        >
          {row.original[count.key]}
        </span>
      ),
    }),
  ),
  column.display({
    id: "problems",
    header: () => <span className="sr-only">Problems</span>,
    cell: ({ row }) =>
      (row.original.error || row.original.warning || row.original.errors.length > 0) && (
        <div className="text-right">
          <RunProblems run={row.original} />
        </div>
      ),
  }),
];

// RecentRuns pages through an integration's runs, newest first, and what each
// one did.
export function RecentRuns({
  identifier,
  search,
}: Readonly<{ identifier: string; search: RunsSearch }>) {
  const navigate = useNavigate();
  const triggers = (search.trigger?.split(",") ?? []) as RunTrigger[];
  const outcomes = (search.outcome?.split(",") ?? []) as RunOutcome[];
  const page = useQuery({
    ...runsQueryOptions(identifier, {
      q: search.q,
      triggers,
      outcomes,
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

  return (
    <DataTable
      noun="runs"
      data={page.data?.items ?? []}
      rowCount={page.data?.total ?? 0}
      loading={page.isPlaceholderData}
      pending={page.isPending}
      error={page.error}
      onRetry={() => void page.refetch()}
      columns={columns}
      getRowId={(run) => String(run.id)}
      search={search}
      searchLabel="Search tokens, actions and errors"
      onClearFilters={() => filter({ trigger: undefined, outcome: undefined })}
      filters={{
        fields: runFilterFields,
        value: {
          ...(outcomes.length > 0 && { outcome: outcomes }),
          ...(triggers.length > 0 && { trigger: triggers }),
        },
        onChange: (id, value) => {
          const values = Array.isArray(value) ? value : [];
          filter({ [id]: values.length > 0 ? values.join(",") : undefined });
        },
      }}
      empty={
        <EmptyState
          icon={<History />}
          title="No runs yet"
          description="Syncs show up here as they start, and pushes and action writes as they finish."
        />
      }
    />
  );
}

function RunProblems({ run }: Readonly<{ run: Run }>) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <IconButton label="View problems">
          <CircleAlert />
        </IconButton>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Problems</DialogTitle>
          <DialogDescription>
            {[
              run.error &&
                "Some or all resources couldn't be fetched, so nothing they write was deleted.",
              run.failed > 0 &&
                `${run.failed} of ${run.received} items weren't written` +
                  (run.failed > run.errors.length
                    ? `; the first ${run.errors.length} are shown`
                    : "") +
                  ".",
              run.warning && "It read everything it could, but warned of the problem below.",
            ]
              .filter(Boolean)
              .join(" ")}
          </DialogDescription>
        </DialogHeader>
        {run.error && <p className="font-mono text-sm break-words">{run.error}</p>}
        {run.warning && <p className="font-mono text-sm break-words">{run.warning}</p>}
        <ul className="grid max-h-[60vh] gap-3 overflow-y-auto">
          {run.errors.map((error) => (
            <li key={error.index} className="grid gap-1 text-sm">
              <span className="font-medium">
                Item {error.index + 1}
                {error.blueprint && (
                  <code className="ml-2 font-mono text-xs text-muted-foreground">
                    {error.blueprint}/{error.identifier || "?"}
                  </code>
                )}
              </span>
              <ul className="list-disc pl-5 text-muted-foreground">
                {error.problems.map((problem) => (
                  <li key={problem}>{problem}</li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  );
}
