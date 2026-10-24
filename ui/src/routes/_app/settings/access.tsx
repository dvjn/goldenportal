import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ShieldCheck, Trash2, User, Users } from "lucide-react";
import { toast } from "sonner";
import { ConfirmIconButton } from "@/components/confirm-icon-button";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  validateTableSearch,
} from "@/components/data-table";
import { GrantAccessDialog } from "@/components/grant-access-dialog";
import { RoleSelect, roleLabels } from "@/components/role-select";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  accessKeys,
  type Grant,
  grantRole,
  grantsQueryOptions,
  revokeGrant,
  useGrants,
} from "@/features/access";
import { type Role, useMe } from "@/features/auth";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/settings/access")({
  validateSearch: validateTableSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(grantsQueryOptions),
  component: AccessPage,
});

const column = dataTableColumns<Grant>();

const columns = [
  column.accessor("name", {
    header: ({ column }) => <DataTableColumnHeader column={column} title="Name" />,
    cell: ({ row }) => <SubjectCell grant={row.original} />,
  }),
  column.accessor((grant) => roleLabels[grant.role], {
    id: "role",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Role" />,
    cell: ({ row }) => <RoleCell grant={row.original} />,
  }),
  column.accessor((grant) => new Date(grant.grantedAt), {
    id: "granted",
    header: ({ column }) => <DataTableColumnHeader column={column} title="Granted" />,
    sortFn: "datetime",
    enableGlobalFilter: false,
    cell: ({ getValue }) => (
      <span className="text-muted-foreground" title={formatAbsolute(getValue())}>
        {formatRelative(getValue())}
      </span>
    ),
  }),
  column.display({
    id: "actions",
    cell: ({ row }) => (
      <div className="flex justify-end">
        <RevokeCell grant={row.original} />
      </div>
    ),
  }),
];

function AccessPage() {
  const grants = useGrants();
  const search = Route.useSearch();

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Roles granted directly to people and groups. Everyone who signs in is a member; people in a
        group get the group's role.
      </p>
      <DataTable
        data={grants}
        columns={columns}
        getRowId={(grant) => `${grant.subjectType}:${grant.subjectId}`}
        search={search}
        searchLabel="Search people and groups"
        actions={<GrantAccessDialog />}
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <ShieldCheck />
              </EmptyMedia>
              <EmptyTitle>No access granted yet</EmptyTitle>
              <EmptyDescription>
                Grant a role to a person or a group to get started.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />
    </div>
  );
}

function SubjectCell({ grant }: Readonly<{ grant: Grant }>) {
  const Icon = grant.subjectType === "group" ? Users : User;
  return (
    <div className="flex items-center gap-3">
      <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <div className="grid">
        <span className="font-medium">{grant.name}</span>
        <span className="text-xs text-muted-foreground">
          {grant.subjectType === "group"
            ? `Group · ${grant.memberCount} ${grant.memberCount === 1 ? "member" : "members"}`
            : grant.email}
        </span>
      </div>
    </div>
  );
}

function useIsMe(grant: Grant) {
  const me = useMe();
  return grant.subjectType === "user" && grant.subjectId === me.id;
}

function RoleCell({ grant }: Readonly<{ grant: Grant }>) {
  const queryClient = useQueryClient();
  const isMe = useIsMe(grant);
  const change = useMutation({
    mutationFn: (role: Role) => grantRole(grant.subjectType, grant.subjectId, role),
    onSuccess: async (_, role) => {
      await queryClient.invalidateQueries({ queryKey: accessKeys.grants });
      toast.success(`${grant.name} is now ${roleLabels[role].toLowerCase()}`);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <RoleSelect
      label={`Role for ${grant.name}`}
      value={grant.role}
      onChange={(role) => change.mutate(role)}
      disabled={isMe || change.isPending}
    />
  );
}

function RevokeCell({ grant }: Readonly<{ grant: Grant }>) {
  const queryClient = useQueryClient();
  const isMe = useIsMe(grant);
  const revoke = useMutation({
    mutationFn: () => revokeGrant(grant.subjectType, grant.subjectId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: accessKeys.grants });
      toast.success(`Removed access for ${grant.name}`);
    },
    onError: (error) => toast.error(error.message),
  });

  if (isMe) return null;

  return (
    <ConfirmIconButton
      label={`Remove access for ${grant.name}`}
      icon={<Trash2 />}
      title={`Remove access for ${grant.name}?`}
      description={
        grant.subjectType === "group"
          ? "Members keep any role granted to them directly or through other groups."
          : "They keep any role they get through their groups."
      }
      confirm="Remove access"
      onConfirm={() => revoke.mutate()}
    />
  );
}
