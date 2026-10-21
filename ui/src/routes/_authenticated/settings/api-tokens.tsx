import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { SecretDialog } from "@/components/secret-dialog";
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
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
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
import { RoleBadge } from "@/features/access";
import {
  type ApiToken,
  apiTokensQueryOptions,
  type TokenRole,
  useApiTokens,
  useCreateApiToken,
  useRevokeApiToken,
} from "@/features/api-tokens";
import { requirePermission, useCan } from "@/features/auth";
import { useSettings } from "@/features/settings";
import { formatDateTime, formatRelative } from "@/lib/time";

export const Route = createFileRoute("/_authenticated/settings/api-tokens")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "access.read"),
  loader: ({ context }) => context.queryClient.ensureQueryData(apiTokensQueryOptions),
  staticData: { breadcrumb: "API tokens" },
  component: ApiTokensPage,
});

function ApiTokensPage() {
  const tokens = useApiTokens();
  const can = useCan();

  return (
    <>
      <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-semibold">API tokens</h1>
          <p className="text-muted-foreground text-sm">
            Machines such as CI call the API with these tokens. A token is a Member or a Maintainer
            and expires within a year.
          </p>
        </div>
        {can("access.write") && <CreateToken />}
      </header>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Created by</TableHead>
            <TableHead>Expires</TableHead>
            <TableHead>Last used</TableHead>
            {can("access.write") && (
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            )}
          </TableRow>
        </TableHeader>
        <TableBody>
          {tokens.map((token) => (
            <TokenRow key={token.id} token={token} />
          ))}
          {tokens.length === 0 && (
            <TableRow>
              <TableCell colSpan={6} className="text-muted-foreground text-center">
                No API tokens yet.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </>
  );
}

function TokenRow({ token }: { token: ApiToken }) {
  const can = useCan();
  const expired = new Date(token.expires_at).getTime() < Date.now();

  return (
    <TableRow>
      <TableCell className="font-medium">{token.name}</TableCell>
      <TableCell>
        <RoleBadge role={token.role} />
      </TableCell>
      <TableCell>{token.created_by || "Unknown"}</TableCell>
      <TableCell title={formatDateTime(token.expires_at)}>
        {expired ? "Expired" : formatRelative(token.expires_at)}
      </TableCell>
      <TableCell>{token.last_used_at ? formatRelative(token.last_used_at) : "Never"}</TableCell>
      {can("access.write") && (
        <TableCell className="text-right">
          <RevokeToken token={token} />
        </TableCell>
      )}
    </TableRow>
  );
}

function RevokeToken({ token }: { token: ApiToken }) {
  const revoke = useRevokeApiToken();

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" size="sm">
          Revoke
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke {token.name}?</AlertDialogTitle>
          <AlertDialogDescription>
            Calls with this token fail at once. You cannot undo this.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {revoke.isError && <p className="text-destructive text-sm">{revoke.error.message}</p>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={revoke.isPending}
            onClick={() => revoke.mutate(token.id)}
          >
            Revoke
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

const expiries = [
  { days: 30, label: "30 days" },
  { days: 90, label: "90 days" },
  { days: 180, label: "180 days" },
  { days: 365, label: "1 year" },
];

function CreateToken() {
  const create = useCreateApiToken();
  const publicUrl = useSettings().public_url;
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [role, setRole] = useState<TokenRole>("member");
  const [days, setDays] = useState("90");
  const [secret, setSecret] = useState<string>();

  function reset(nextOpen: boolean) {
    setOpen(nextOpen);
    setName("");
    setRole("member");
    setDays("90");
    create.reset();
  }

  function submit(event: React.FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, role, expires_in_days: Number(days) },
      {
        onSuccess: (created) => {
          reset(false);
          setSecret(created.secret);
        },
      },
    );
  }

  return (
    <>
      <Dialog open={open} onOpenChange={reset}>
        <DialogTrigger asChild>
          <Button>New token</Button>
        </DialogTrigger>
        <DialogContent>
          <form onSubmit={submit} className="flex flex-col gap-6">
            <DialogHeader>
              <DialogTitle>New API token</DialogTitle>
              <DialogDescription>
                Name the machine that uses it, so you know what breaks when you revoke it.
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="token-name">Name</FieldLabel>
                <Input
                  id="token-name"
                  required
                  maxLength={100}
                  placeholder="gitlab-ci"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="token-role">Role</FieldLabel>
                <Select value={role} onValueChange={(value) => setRole(value as TokenRole)}>
                  <SelectTrigger id="token-role">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">Member, reads the catalog</SelectItem>
                    <SelectItem value="maintainer">Maintainer, edits the catalog</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="token-expiry">Expires in</FieldLabel>
                <Select value={days} onValueChange={setDays}>
                  <SelectTrigger id="token-expiry">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {expiries.map((expiry) => (
                      <SelectItem key={expiry.days} value={String(expiry.days)}>
                        {expiry.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              {create.isError && <FieldError>{create.error.message}</FieldError>}
            </FieldGroup>
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline">
                  Cancel
                </Button>
              </DialogClose>
              <Button type="submit" disabled={create.isPending || name.trim() === ""}>
                Create token
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <SecretDialog title="New API token" secret={secret} onClose={() => setSecret(undefined)}>
        <p className="text-muted-foreground text-sm">
          Send it as a bearer token, for example:{" "}
          <code className="font-mono break-all">
            curl -H "Authorization: Bearer $TOKEN" {publicUrl}/api/v1/auth/me
          </code>
        </p>
      </SecretDialog>
    </>
  );
}
