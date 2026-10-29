import { createFileRoute } from "@tanstack/react-router";
import { cn } from "cn";
import { PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { PaginationBar } from "@/components/common/pagination-bar";
import { StateCard } from "@/components/common/state-card";
import { TableFrame } from "@/components/common/table-frame";
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
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { RoleBadge } from "@/features/access";
import { requirePermission, useCan } from "@/features/auth";
import {
  type ServiceToken,
  serviceTokensQueryOptions,
  type TokenRole,
  useCreateServiceToken,
  useRevokeServiceToken,
  useServiceTokens,
} from "@/features/service-tokens";
import { RelativeTime, useSettings } from "@/features/settings";
import { formatDateTime, formatRelative } from "@/lib/time";
import { usePageTitle } from "@/lib/use-page-title";

export const Route = createFileRoute("/_authenticated/settings/service-tokens")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "access.read"),
  loader: ({ context }) => context.queryClient.ensureQueryData(serviceTokensQueryOptions),
  pendingComponent: ListSkeleton,
  component: ServiceTokensPage,
});

const pageSize = 25;
const soonSeconds = 7 * 24 * 3600;

function ServiceTokensPage() {
  usePageTitle("Service tokens");
  const tokens = useServiceTokens();
  const can = useCan();
  const [page, setPage] = useState(1);
  const shown = tokens.slice((page - 1) * pageSize, page * pageSize);

  return (
    <>
      <PageHeader
        title="Service tokens"
        description="Tokens for CI jobs, scripts and the CLI. Each has its own role and works independently of the person who created it."
        actions={can("access.write") && <CreateToken />}
      />
      {tokens.length === 0 ? (
        <StateCard
          title="No service tokens"
          message="Create a token for each CI job, script or tool that calls the portal."
        />
      ) : (
        <TableFrame
          footer={
            <PaginationBar
              page={page}
              pageSize={pageSize}
              total={tokens.length}
              onPageChange={setPage}
            />
          }
        >
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Created by</TableHead>
              <TableHead>Created</TableHead>
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
            {shown.map((token) => (
              <TokenRow key={token.id} token={token} />
            ))}
          </TableBody>
        </TableFrame>
      )}
    </>
  );
}

function TokenRow({ token }: { token: ServiceToken }) {
  const can = useCan();

  return (
    <TableRow>
      <TableCell className="font-medium">{token.name}</TableCell>
      <TableCell>
        <RoleBadge role={token.role} />
      </TableCell>
      <TableCell>{token.created_by || "Unknown"}</TableCell>
      <TableCell className="text-muted-foreground">
        <RelativeTime at={token.created_at} />
      </TableCell>
      <TableCell>
        <Expiry at={token.expires_at} />
      </TableCell>
      <TableCell className="text-muted-foreground">
        <RelativeTime at={token.last_used_at} empty="Never" />
      </TableCell>
      {can("access.write") && (
        <TableCell className="text-right">
          <RevokeToken token={token} />
        </TableCell>
      )}
    </TableRow>
  );
}

function Expiry({ at }: { at: string }) {
  const secondsLeft = (new Date(at).getTime() - Date.now()) / 1000;
  const expired = secondsLeft <= 0;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time
          dateTime={at}
          className={cn(
            expired && "text-destructive",
            !expired && secondsLeft < soonSeconds && "font-medium text-warning",
          )}
        >
          {expired ? "Expired" : formatRelative(at)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatDateTime(at)}</TooltipContent>
    </Tooltip>
  );
}

function RevokeToken({ token }: { token: ServiceToken }) {
  const revoke = useRevokeServiceToken();

  function confirm() {
    revoke.mutate(token.id, {
      onSuccess: () => toast.success(`Revoked ${token.name}`),
      onError: (error) => toast.error(error.message),
    });
  }

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
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={revoke.isPending} onClick={confirm}>
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
  const create = useCreateServiceToken();
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
          <Button>
            <PlusIcon />
            New service token
          </Button>
        </DialogTrigger>
        <DialogContent>
          <form onSubmit={submit} className="flex flex-col gap-6">
            <DialogHeader>
              <DialogTitle>New service token</DialogTitle>
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
      <SecretDialog title="New service token" secret={secret} onClose={() => setSecret(undefined)}>
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
