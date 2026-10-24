import { createFileRoute } from "@tanstack/react-router";
import { KeyRound } from "lucide-react";
import { CreateTokenDialog } from "@/components/create-token-dialog";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { RevokeTokenButton } from "@/components/revoke-token-button";
import { Badge } from "@/components/ui/badge";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { type ApiToken, tokensQueryOptions, useTokens } from "@/features/tokens";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/account/tokens")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(tokensQueryOptions),
  component: TokensPage,
});

const column = dataTableColumns<ApiToken>();

const columns = [
  column.accessor("name", {
    header: ({ column }) => <DataTableColumnHeader column={column} title="Name" />,
    cell: ({ getValue }) => <span className="font-medium">{getValue()}</span>,
  }),
  column.accessor("prefix", {
    header: "Token",
    enableSorting: false,
    cell: ({ getValue }) => <span className="font-mono text-muted-foreground">{getValue()}…</span>,
  }),
  column.accessor((token) => (token.readOnly ? "Read only" : "Read and write"), {
    id: "access",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Access" />,
    cell: ({ getValue, row }) => (
      <Badge variant={row.original.readOnly ? "secondary" : "outline"}>{getValue()}</Badge>
    ),
  }),
  column.accessor((token) => new Date(token.expiresAt), {
    id: "expires",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Expires" />,
    sortFn: "datetime",
    enableGlobalFilter: false,
    cell: ({ getValue }) => {
      const expiresAt = getValue();
      return (
        <span title={formatAbsolute(expiresAt)}>
          {expiresAt <= new Date() ? (
            <Badge variant="destructive">Expired</Badge>
          ) : (
            formatRelative(expiresAt)
          )}
        </span>
      );
    },
  }),
  column.accessor((token) => (token.lastUsedAt ? new Date(token.lastUsedAt) : undefined), {
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
          {lastUsed ? formatRelative(lastUsed) : "Never"}
        </span>
      );
    },
  }),
  column.display({
    id: "actions",
    cell: ({ row }) => (
      <div className="flex justify-end">
        <RevokeTokenButton token={row.original} />
      </div>
    ),
  }),
];

function TokensPage() {
  const tokens = useTokens();
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="API Tokens"
        description="Tokens let scripts and CI jobs call the Golden Portal API as you."
        actions={<CreateTokenDialog />}
      />
      <DataTable
        data={tokens}
        columns={columns}
        getRowId={(token) => token.id}
        search={search}
        searchLabel="Search tokens"
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <KeyRound />
              </EmptyMedia>
              <EmptyTitle>No API tokens yet</EmptyTitle>
              <EmptyDescription>
                Create one to call the API from a script or a CI job.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />
    </div>
  );
}
