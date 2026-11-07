import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ScrollText } from "lucide-react";
import {
  DataTable,
  type DataTableFilterField,
  type DateRange,
  dataTableColumns,
  isDuration,
  pageSize,
  type TableSearch,
  usePageSize,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { roleLabels } from "@/components/role-select";
import { Badge } from "@/components/ui/badge";
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
  within?: string;
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
    within: isDuration(search.within) ? search.within : undefined,
  };
}

function filtersFrom(search: AuditSearch, size: number): AuditFilters {
  return {
    q: search.q,
    actions: search.action?.split(","),
    from: search.from,
    to: search.to,
    within: search.within,
    page: search.page,
    size,
  };
}

export const Route = createFileRoute("/_app/settings/audit")({
  validateSearch: validateAuditSearch,
  loaderDeps: ({ search }) => filtersFrom(search, pageSize(search)),
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
  column.accessor("occurredAt", {
    header: "When",
    enableSorting: false,
    cell: ({ getValue }) => (
      <span className="whitespace-nowrap text-muted-foreground" title={formatAbsolute(getValue())}>
        {formatRelative(getValue())}
      </span>
    ),
  }),
  column.accessor((event) => event.actor.name || event.actor.label, {
    id: "who",
    header: "Who",
    enableSorting: false,
    // People read as on Access: their name, over their email.
    cell: ({ row }) => {
      const { name, label } = row.original.actor;
      if (!name) return label;
      return (
        <span className="grid">
          <span>{name}</span>
          <span className="text-xs text-muted-foreground">{label}</span>
        </span>
      );
    },
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

const auditFilterFields: DataTableFilterField[] = [
  { kind: "options", id: "action", title: "Action", useOptions: () => actionOptions },
  { kind: "date", id: "date", title: "Date" },
];

function AuditPage() {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data, isFetching, isPending, error, refetch } = useAuditEvents(
    filtersFrom(search, usePageSize(search)),
  );
  const actions = search.action?.split(",") ?? [];
  const events = data?.items ?? [];

  function filter(patch: Partial<AuditSearch>) {
    void navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
      replace: true,
      resetScroll: false,
    });
  }

  return (
    <div className="flex flex-col gap-4">
      <DataTable
        noun="events"
        data={events}
        rowCount={data?.total ?? 0}
        loading={isFetching}
        pending={isPending}
        error={error}
        onRetry={() => void refetch()}
        columns={
          events.some((event) => describeDetails(event))
            ? columns
            : columns.filter((c) => c.id !== "details")
        }
        getRowId={(event) => String(event.id)}
        search={search}
        searchLabel="Search people and targets"
        onClearFilters={() =>
          filter({ action: undefined, from: undefined, to: undefined, within: undefined })
        }
        filters={{
          fields: auditFilterFields,
          value: {
            ...(actions.length > 0 && { action: actions }),
            ...((search.from || search.to || search.within) && {
              date: { from: search.from, to: search.to, within: search.within },
            }),
          },
          onChange: (id, value) => {
            if (id === "date") {
              const range: DateRange = Array.isArray(value) ? {} : (value ?? {});
              filter({ from: range.from, to: range.to, within: range.within });
              return;
            }
            const values = Array.isArray(value) ? value : [];
            filter({ action: values.length > 0 ? values.join(",") : undefined });
          },
        }}
        empty={
          <EmptyState
            icon={<ScrollText />}
            title="No events yet"
            description="Events appear here as people sign in and make changes."
          />
        }
      />
    </div>
  );
}
