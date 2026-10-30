import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ScrollText } from "lucide-react";
import {
  DataTable,
  DataTableDateFilter,
  DataTableFacetedFilter,
  type DateRange,
  dataTableColumns,
  type TableSearch,
  validateTableSearch,
} from "@/components/data-table";
import { roleLabels } from "@/components/role-select";
import { Badge } from "@/components/ui/badge";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  type AuditEvent,
  type AuditFilters,
  auditActions,
  auditQueryOptions,
  useAuditEvents,
} from "@/features/audit";
import type { Role } from "@/features/auth";
import { formatAbsolute, formatRelative } from "@/lib/format";

type AuditSearch = TableSearch & {
  // action is a comma-separated list of actions to show.
  action?: string;
  from?: string;
  to?: string;
};

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

function validateAuditSearch(search: Record<string, unknown>): AuditSearch {
  const date = (value: unknown) =>
    typeof value === "string" && datePattern.test(value) ? value : undefined;
  const actions =
    typeof search.action === "string"
      ? search.action.split(",").filter((action) => action in auditActions)
      : [];
  return {
    ...validateTableSearch(search),
    sort: undefined,
    action: actions.length > 0 ? actions.join(",") : undefined,
    from: date(search.from),
    to: date(search.to),
  };
}

function filtersFrom(search: AuditSearch): AuditFilters {
  return {
    q: search.q,
    actions: search.action?.split(","),
    from: search.from,
    to: search.to,
    page: search.page,
    size: search.size,
  };
}

export const Route = createFileRoute("/_app/settings/audit")({
  validateSearch: validateAuditSearch,
  loaderDeps: ({ search }) => filtersFrom(search),
  loader: ({ context, deps }) => context.queryClient.ensureQueryData(auditQueryOptions(deps)),
  component: AuditPage,
});

function roleText(role: unknown) {
  return typeof role === "string" ? (roleLabels[role as Role] ?? role) : "none";
}

// describeDetails turns an event's details into a short sentence.
function describeDetails(event: AuditEvent) {
  const { details } = event;
  switch (event.action) {
    case "access.grant":
    case "access.bootstrap_admin":
      return `Role: ${roleText(details.role)}`;
    case "token.create":
      return details.readOnly ? "Read only" : "Read and write";
    default:
      return "";
  }
}

const column = dataTableColumns<AuditEvent>();

const columns = [
  column.accessor("at", {
    header: "When",
    enableSorting: false,
    cell: ({ getValue }) => (
      <span className="whitespace-nowrap text-muted-foreground" title={formatAbsolute(getValue())}>
        {formatRelative(getValue())}
      </span>
    ),
  }),
  column.accessor((event) => event.actor.label, {
    id: "who",
    header: "Who",
    enableSorting: false,
  }),
  column.accessor((event) => auditActions[event.action] ?? event.action, {
    id: "what",
    header: "What",
    enableSorting: false,
    cell: ({ getValue }) => <span className="font-medium">{getValue()}</span>,
  }),
  column.accessor((event) => event.target.label, {
    id: "target",
    header: "Target",
    enableSorting: false,
    cell: ({ getValue, row }) => (
      <span className="flex items-center gap-2">
        {getValue()}
        {row.original.target.type && (
          <Badge variant="outline" className="capitalize">
            {row.original.target.type}
          </Badge>
        )}
      </span>
    ),
  }),
  column.display({
    id: "details",
    header: "Details",
    cell: ({ row }) => (
      <span className="text-muted-foreground">{describeDetails(row.original)}</span>
    ),
  }),
];

const actionOptions = Object.entries(auditActions).map(([value, label]) => ({ value, label }));

function AuditPage() {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data, isFetching, isPending, error, refetch } = useAuditEvents(filtersFrom(search));
  const actions = search.action?.split(",") ?? [];
  const filtered = actions.length > 0 || Boolean(search.from || search.to);

  function filter(patch: Partial<AuditSearch>) {
    void navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
      replace: true,
      resetScroll: false,
    });
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Sign-ins, API tokens and access changes, newest first. Events are also written to the server
        log.
      </p>
      <DataTable
        data={data?.events ?? []}
        rowCount={data?.total ?? 0}
        loading={isFetching}
        pending={isPending}
        error={error}
        onRetry={() => void refetch()}
        columns={columns}
        getRowId={(event) => String(event.id)}
        search={search}
        searchLabel="Search people and targets"
        filtered={filtered}
        onClearFilters={() => filter({ action: undefined, from: undefined, to: undefined })}
        filters={
          <>
            <DataTableFacetedFilter
              title="Action"
              options={actionOptions}
              selected={actions}
              onChange={(next) => filter({ action: next.length > 0 ? next.join(",") : undefined })}
            />
            <DataTableDateFilter
              title="Date"
              value={{ from: search.from, to: search.to }}
              onChange={(range: DateRange) => filter({ from: range.from, to: range.to })}
            />
          </>
        }
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ScrollText />
              </EmptyMedia>
              <EmptyTitle>Nothing recorded yet</EmptyTitle>
              <EmptyDescription>
                Events appear here as people sign in and make changes.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />
    </div>
  );
}
