import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useState } from "react";
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
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  type Grant,
  GrantDialog,
  type GrantRole,
  grantsQueryOptions,
  RoleBadge,
  type Subject,
  SubjectPicker,
  subjectsQueryOptions,
  useGrants,
  useRemoveGrant,
  useRevokeSessions,
  userCheckQueryOptions,
  useSubjects,
  useUserCheck,
} from "@/features/access";
import { requirePermission, useCan } from "@/features/auth";
import { scimQueryOptions, useScim } from "@/features/settings";
import { formatRelative } from "@/lib/time";

type AccessSearch = { user?: string };

export const Route = createFileRoute("/_authenticated/settings/access")({
  validateSearch: (search: Record<string, unknown>): AccessSearch => ({
    user: typeof search.user === "string" ? search.user : undefined,
  }),
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "access.read"),
  loaderDeps: ({ search }) => ({ user: search.user }),
  loader: ({ context, deps }) =>
    Promise.all([
      context.queryClient.ensureQueryData(grantsQueryOptions),
      context.queryClient.ensureQueryData(subjectsQueryOptions),
      context.queryClient.ensureQueryData(scimQueryOptions),
      deps.user && context.queryClient.ensureQueryData(userCheckQueryOptions(deps.user)),
    ]),
  staticData: { breadcrumb: "Access" },
  component: AccessPage,
});

type RoleFilter = "all" | GrantRole;

function AccessPage() {
  const grants = useGrants();
  const scim = useScim();
  const can = useCan();
  const [query, setQuery] = useState("");
  const [roleFilter, setRoleFilter] = useState<RoleFilter>("all");
  const [dialog, setDialog] = useState<{ open: boolean; subject?: Subject }>({ open: false });
  const needle = query.trim().toLowerCase();
  const shown = grants.filter(
    (grant) =>
      (roleFilter === "all" || grant.role === roleFilter) &&
      `${grant.subject.name} ${grant.subject.email ?? ""}`.toLowerCase().includes(needle),
  );

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">Access</h1>
        <p className="text-muted-foreground text-sm">
          Who holds Admin or Maintainer. Everyone that SCIM provisions is a Member.
        </p>
      </header>
      <div className="flex flex-col gap-2 sm:flex-row">
        <InputGroup className="sm:max-w-xs">
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
          <InputGroupInput
            aria-label="Search grants"
            placeholder="Search users and groups"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </InputGroup>
        <Select value={roleFilter} onValueChange={(value) => setRoleFilter(value as RoleFilter)}>
          <SelectTrigger aria-label="Filter by role" className="sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All roles</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
            <SelectItem value="maintainer">Maintainer</SelectItem>
          </SelectContent>
        </Select>
        {can("access.write") && (
          <Button className="sm:ml-auto" onClick={() => setDialog({ open: true })}>
            Grant access
          </Button>
        )}
      </div>
      <p className="text-muted-foreground text-sm">
        Users and groups sync from SCIM,{" "}
        {scim.last_push_at ? `last push ${formatRelative(scim.last_push_at)}` : "no push yet"}.{" "}
        {grants.length === 1 ? "1 grant" : `${grants.length} grants`}.
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Subject</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Granted by</TableHead>
            {can("access.write") && (
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            )}
          </TableRow>
        </TableHeader>
        <TableBody>
          {shown.map((grant) => (
            <GrantRow
              key={grant.id}
              grant={grant}
              onChangeRole={() => setDialog({ open: true, subject: grant.subject })}
            />
          ))}
          {shown.length === 0 && (
            <TableRow>
              <TableCell colSpan={4} className="text-muted-foreground text-center">
                {grants.length === 0 ? "No grants yet." : "No grant matches the search."}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <CheckUserCard />
      <GrantDialog
        open={dialog.open}
        subject={dialog.subject}
        onOpenChange={(open) => setDialog({ open })}
      />
    </>
  );
}

function GrantRow({ grant, onChangeRole }: { grant: Grant; onChangeRole: () => void }) {
  const can = useCan();
  const { subject } = grant;

  return (
    <TableRow>
      <TableCell>
        <div className="flex flex-col">
          <span className="font-medium">{subject.name}</span>
          <span className="text-muted-foreground text-xs">
            {subject.type === "group"
              ? `Group, ${subject.member_count === 1 ? "1 member" : `${subject.member_count} members`}`
              : `User, ${subject.email}`}
          </span>
        </div>
      </TableCell>
      <TableCell>
        <RoleBadge role={grant.role} />
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{grant.granted_by || "Unknown"}</span>
          <span className="text-muted-foreground text-xs">{formatRelative(grant.updated_at)}</span>
        </div>
      </TableCell>
      {can("access.write") && (
        <TableCell>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={onChangeRole}>
              Change role
            </Button>
            <RemoveGrant grant={grant} />
          </div>
        </TableCell>
      )}
    </TableRow>
  );
}

function RemoveGrant({ grant }: { grant: Grant }) {
  const remove = useRemoveGrant();

  if (grant.last_admin) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span>
            <Button variant="outline" size="sm" disabled>
              Remove
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent>The last Admin grant cannot be removed</TooltipContent>
      </Tooltip>
    );
  }

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" size="sm">
          Remove
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Remove the grant for {grant.subject.name}?</AlertDialogTitle>
          <AlertDialogDescription>
            {grant.subject.type === "group" ? "Its members go back" : "This user goes back"} to the
            Member role unless another grant gives them more.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {remove.isError && <p className="text-destructive text-sm">{remove.error.message}</p>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              event.preventDefault();
              remove.mutate(grant.subject);
            }}
          >
            Remove
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function CheckUserCard() {
  const subjects = useSubjects();
  const { user } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Check a user</CardTitle>
        <CardDescription>See the effective role of any provisioned user.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="sm:max-w-sm">
          <SubjectPicker
            aria-label="User to check"
            subjects={subjects}
            usersOnly
            value={user ? { type: "user", id: user } : undefined}
            onChange={(picked) => navigate({ search: { user: picked.id }, replace: true })}
            placeholder="Pick a user"
          />
        </div>
        {user && <UserCheckResult id={user} />}
      </CardContent>
    </Card>
  );
}

function UserCheckResult({ id }: { id: string }) {
  const check = useUserCheck(id);
  const can = useCan();

  return (
    <div className="flex flex-col gap-2 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{check.user.name}</span>
        {check.role && <RoleBadge role={check.role} />}
        <span className="text-muted-foreground">{explain(check)}</span>
        {can("access.write") && <RevokeSessions id={check.user.id} name={check.user.name} />}
      </div>
      <p className="text-muted-foreground">
        Groups:{" "}
        {check.groups.length > 0 ? check.groups.map((group) => group.name).join(", ") : "none"}.{" "}
        {check.user.active ? "Active" : "Inactive"},{" "}
        {check.last_sign_in_at
          ? `last sign-in ${formatRelative(check.last_sign_in_at)}`
          : "never signed in"}
        .
      </p>
    </div>
  );
}

function explain(check: ReturnType<typeof useUserCheck>) {
  if (!check.user.active) return "Inactive in SCIM, cannot sign in.";
  if (check.bootstrap) return "Admin through GOLDPORT_BOOTSTRAP_ADMINS. No grant gives it.";
  if (check.carried_by.length === 0) return "No grant. Every provisioned user is a Member.";
  return `Through ${check.carried_by
    .map((carrier) => (carrier.type === "group" ? carrier.name : "a direct grant"))
    .join(" and ")}.`;
}

function RevokeSessions({ id, name }: { id: string; name: string }) {
  const revoke = useRevokeSessions();

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" size="sm" className="sm:ml-auto">
          {revoke.isSuccess ? "Sessions revoked" : "Revoke sessions"}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke the sessions of {name}?</AlertDialogTitle>
          <AlertDialogDescription>
            They are signed out everywhere and have to sign in again.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {revoke.isError && <p className="text-destructive text-sm">{revoke.error.message}</p>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={revoke.isPending} onClick={() => revoke.mutate(id)}>
            Revoke sessions
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
