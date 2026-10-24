import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { UsersRound } from "lucide-react";
import { toast } from "sonner";
import { RoleSelect } from "@/components/role-select";
import { Card } from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  accessKeys,
  type Group,
  type GroupUpdate,
  groupsQueryOptions,
  updateGroup,
  useGroups,
} from "@/features/access";

export const Route = createFileRoute("/_app/settings/groups")({
  loader: ({ context }) => context.queryClient.ensureQueryData(groupsQueryOptions),
  component: GroupsPage,
});

function GroupsPage() {
  const groups = useGroups();

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-1">
        <h2 className="text-lg font-semibold">Groups</h2>
        <p className="text-sm text-muted-foreground">
          Groups come from your identity provider and update each time a member signs in. Give a
          group a role to grant it to every member, and mark it as a team so it can own things.
        </p>
      </div>
      <Card className="py-0">
        {groups.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersRound />
              </EmptyMedia>
              <EmptyTitle>No groups yet</EmptyTitle>
              <EmptyDescription>
                Groups appear when someone signs in and the identity provider lists their groups.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Name</TableHead>
                <TableHead>Members</TableHead>
                <TableHead>Role</TableHead>
                <TableHead className="pr-4">Team</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.map((group) => (
                <GroupRow key={group.id} group={group} />
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  );
}

function GroupRow({ group }: Readonly<{ group: Group }>) {
  const queryClient = useQueryClient();
  const update = useMutation({
    mutationFn: (change: Partial<GroupUpdate>) =>
      updateGroup(group.id, { role: group.role, isTeam: group.isTeam, ...change }),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: accessKeys.groups }),
        queryClient.invalidateQueries({ queryKey: accessKeys.users }),
      ]),
    onError: (error) => toast.error(error.message),
  });

  return (
    <TableRow>
      <TableCell className="pl-4 font-medium">{group.name}</TableCell>
      <TableCell className="tabular-nums">{group.memberCount}</TableCell>
      <TableCell>
        <RoleSelect
          label={`Role for ${group.name}`}
          value={group.role}
          noneLabel="No role"
          onChange={(role) => update.mutate({ role })}
          disabled={update.isPending}
        />
      </TableCell>
      <TableCell className="pr-4">
        <Switch
          checked={group.isTeam}
          onCheckedChange={(isTeam) => update.mutate({ isTeam })}
          disabled={update.isPending}
          aria-label={`${group.name} is a team`}
        />
      </TableCell>
    </TableRow>
  );
}
