import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AppWindow, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { AppLogo } from "@/components/app-logo";
import { EmptyValue } from "@/components/catalog/property-value";
import { Code } from "@/components/code";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { IconButton } from "@/components/icon-button";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { useConfig } from "@/features/app-config";
import {
  type AppGrant,
  appKeys,
  appsQueryOptions,
  revokeApp,
  scopeLabels,
  useApps,
} from "@/features/apps";
import { hasRole, useMe } from "@/features/auth";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/account/apps")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(appsQueryOptions),
  component: AppsPage,
});

// scopeNames are the short names access shows as, with the full label on hover.
const scopeNames: Record<string, string> = {
  "portal:read": "Read",
  "portal:run": "Run",
  "portal:author": "Author",
};

const column = dataTableColumns<AppGrant>();

const columns = [
  column.accessor("client", {
    header: ({ column }) => <DataTableColumnHeader column={column} title="App" />,
    cell: ({ getValue, row }) => (
      <span className="flex items-center gap-2 font-medium">
        <AppLogo logo={row.original.logo} size="sm" />
        {getValue()}
      </span>
    ),
  }),
  column.accessor("server", {
    header: ({ column }) => <DataTableColumnHeader column={column} title="Server" />,
    cell: ({ getValue }) => <span className="text-muted-foreground">{getValue()}</span>,
  }),
  column.accessor((app) => app.scopes.join(" "), {
    id: "access",
    header: "Access",
    enableSorting: false,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <span className="flex gap-1">
        {row.original.scopes.map((scope) => (
          <Badge key={scope} variant="outline" title={scopeLabels[scope]}>
            {scopeNames[scope] ?? scope}
          </Badge>
        ))}
      </span>
    ),
  }),
  column.accessor((app) => new Date(app.createdAt), {
    id: "authorized",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Authorized" />,
    sortFn: "datetime",
    enableGlobalFilter: false,
    cell: ({ getValue }) => (
      <span className="text-muted-foreground" title={formatAbsolute(getValue())}>
        {formatRelative(getValue())}
      </span>
    ),
  }),
  column.accessor((app) => (app.lastUsedAt ? new Date(app.lastUsedAt) : undefined), {
    id: "lastUsed",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Last used" />,
    sortFn: "datetime",
    sortUndefined: "last",
    enableGlobalFilter: false,
    cell: ({ getValue }) => {
      const lastUsed = getValue();
      return (
        <span
          className="text-muted-foreground"
          title={lastUsed ? formatAbsolute(lastUsed) : undefined}
        >
          {lastUsed ? formatRelative(lastUsed) : <EmptyValue title="Never used" />}
        </span>
      );
    },
  }),
  column.display({
    id: "actions",
    cell: ({ row }) => (
      <div className="flex justify-end">
        <RevokeAppButton app={row.original} />
      </div>
    ),
  }),
];

function RevokeAppButton({ app }: Readonly<{ app: AppGrant }>) {
  const queryClient = useQueryClient();
  const revoke = useMutation({
    mutationFn: () => revokeApp(app.id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: appKeys.all });
      toast.success(`Revoked ${app.client}`);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <IconButton label={`Revoke ${app.client}`}>
          <Trash2 />
        </IconButton>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke "{app.client}"?</AlertDialogTitle>
          <AlertDialogDescription>
            It's signed out right away and has to ask you again to reconnect.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => revoke.mutate()}>
            Revoke Access
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

// AppsPage lists the apps, such as MCP clients, the user let act as them,
// and where to connect one.
function AppsPage() {
  const apps = useApps();
  const search = Route.useSearch();
  const { mcp } = useConfig();
  const builder = hasRole(useMe().role, "builder");

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Connected Apps" icon={<PageIcon page="apps" />} />
      {mcp && <McpServers user={mcp.user} builder={builder ? mcp.builder : undefined} />}
      <DataTable
        noun="apps"
        data={apps}
        columns={columns}
        getRowId={(app) => app.id}
        search={search}
        searchLabel="Search apps"
        empty={
          <EmptyState
            icon={<AppWindow />}
            title="No apps yet"
            description="Apps you authorize, such as AI assistants connecting over MCP, show up here."
          />
        }
      />
    </div>
  );
}

// McpServers says where an AI assistant connects: the user's server, and
// for builders, the one that changes configuration.
function McpServers({ user, builder }: Readonly<{ user: string; builder?: string }>) {
  return (
    <div className="grid gap-1 text-sm text-muted-foreground">
      <p>
        Connect an AI assistant over MCP at <Code>{user}</Code> to read the portal and run actions
        as you.
      </p>
      {builder && (
        <p>
          Connect it at <Code>{builder}</Code> to change the portal's configuration.
        </p>
      )}
    </div>
  );
}
