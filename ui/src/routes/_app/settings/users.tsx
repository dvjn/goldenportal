import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { toast } from "sonner";
import { RoleSelect, roleLabels } from "@/components/role-select";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { accessKeys, setUserRole, type User, usersQueryOptions, useUsers } from "@/features/access";
import { type Role, useMe } from "@/features/auth";
import { formatAbsolute, formatRelative } from "@/lib/format";

export const Route = createFileRoute("/_app/settings/users")({
  loader: ({ context }) => context.queryClient.ensureQueryData(usersQueryOptions),
  component: UsersPage,
});

function UsersPage() {
  const users = useUsers();
  const me = useMe();

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-1">
        <h2 className="text-lg font-semibold">Users</h2>
        <p className="text-sm text-muted-foreground">
          People appear here after they first sign in. Each person gets the highest of the role set
          here and the roles of their groups.
        </p>
      </div>
      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Name</TableHead>
              <TableHead>Groups</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Effective role</TableHead>
              <TableHead className="pr-4">Last sign-in</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((user) => (
              <UserRow key={user.id} user={user} isMe={user.id === me.id} />
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}

function UserRow({ user, isMe }: Readonly<{ user: User; isMe: boolean }>) {
  const queryClient = useQueryClient();
  const update = useMutation({
    mutationFn: (role: Role | null) => setUserRole(user.id, role),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: accessKeys.users }),
    onError: (error) => toast.error(error.message),
  });

  return (
    <TableRow>
      <TableCell className="pl-4">
        <div className="grid">
          <span className="font-medium">
            {user.displayName}
            {isMe && <span className="text-muted-foreground"> (you)</span>}
          </span>
          <span className="text-xs text-muted-foreground">{user.email}</span>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {user.groups.length === 0 ? (
            <span className="text-muted-foreground">None</span>
          ) : (
            user.groups.map((group) => (
              <Badge key={group} variant="outline">
                {group}
              </Badge>
            ))
          )}
        </div>
      </TableCell>
      <TableCell>
        <RoleSelect
          label={`Role for ${user.displayName}`}
          value={user.role}
          noneLabel="From groups"
          onChange={(role) => update.mutate(role)}
          disabled={isMe || update.isPending}
        />
      </TableCell>
      <TableCell>
        <Badge variant={user.effectiveRole === "member" ? "secondary" : "default"}>
          {roleLabels[user.effectiveRole]}
        </Badge>
      </TableCell>
      <TableCell
        className="pr-4 text-muted-foreground"
        title={user.lastLoginAt ? formatAbsolute(user.lastLoginAt) : undefined}
      >
        {user.lastLoginAt ? formatRelative(user.lastLoginAt) : "Never"}
      </TableCell>
    </TableRow>
  );
}
