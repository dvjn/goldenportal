import { useState } from "react";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { StateCard } from "@/components/common/state-card";
import { TableFrame } from "@/components/common/table-frame";
import { CopyButton } from "@/components/copy-button";
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
import {
  type CreatedIngestToken,
  type IngestToken,
  type IntegrationDetail,
  useCreateIngestToken,
  useIngestTokens,
  useRevokeIngestToken,
} from "@/features/integrations";
import { formatDateTime, formatRelative } from "@/lib/time";

export function IngestTokens({ integration }: { integration: IntegrationDetail }) {
  const tokens = useIngestTokens(integration.identifier);

  return (
    <section aria-label="Ingest tokens" className="flex flex-col gap-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex flex-col gap-1">
          <h3 className="text-sm font-medium">Ingest tokens</h3>
          <p className="text-muted-foreground text-sm">
            A token works only for this integration and expires within a year.
          </p>
        </div>
        <CreateIngestToken integration={integration} />
      </div>
      {tokens.isError ? (
        <StateCard title="Ingest tokens unavailable" message="We could not load the tokens." />
      ) : tokens.data ? (
        <TableFrame>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Created by</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead>Last used</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tokens.data.map((token) => (
              <TableRow key={token.id}>
                <TableCell className="font-medium">{token.name}</TableCell>
                <TableCell>{token.created_by || "Unknown"}</TableCell>
                <TableCell title={formatDateTime(token.expires_at)}>
                  {new Date(token.expires_at).getTime() < Date.now()
                    ? "Expired"
                    : formatRelative(token.expires_at)}
                </TableCell>
                <TableCell>
                  {token.last_used_at ? formatRelative(token.last_used_at) : "Never"}
                </TableCell>
                <TableCell className="text-right">
                  <RevokeIngestToken integration={integration} token={token} />
                </TableCell>
              </TableRow>
            ))}
            {tokens.data.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground h-16 text-center">
                  No ingest tokens yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </TableFrame>
      ) : (
        <ListSkeleton />
      )}
    </section>
  );
}

function RevokeIngestToken({
  integration,
  token,
}: {
  integration: IntegrationDetail;
  token: IngestToken;
}) {
  const revoke = useRevokeIngestToken(integration.identifier);

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
            Pushes with this token fail at once. You cannot undo this.
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

// pushCommand is a ready curl call that pushes the entities in a file to
// the integration's first kind.
export function pushCommand(integration: IntegrationDetail, created: CreatedIngestToken) {
  const kind = integration.kinds[0]?.kind ?? "<kind>";
  return [
    `curl -X POST ${created.ingest_url}/${kind}`,
    `  -H "Authorization: Bearer ${created.secret}"`,
    `  -H "Content-Type: application/json"`,
    `  -d @${kind}.json`,
  ].join(" \\\n");
}

function payloadHint(integration: IntegrationDetail) {
  const kind = integration.kinds[0];
  if (!kind) return "Add a mapping to the integration first, so it knows what it receives.";
  const entity = kind.canonical
    ? "an entity with identifier, title, properties and relations"
    : `a record the ${kind.kind} mapping reads`;
  return `${kind.kind}.json holds {"mode": "upsert", "entities": [...]}, each item ${entity}. A snapshot also deletes the ${kind.blueprint_title} entities it leaves out.`;
}

function CreateIngestToken({ integration }: { integration: IntegrationDetail }) {
  const create = useCreateIngestToken(integration.identifier);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [days, setDays] = useState("90");
  const [created, setCreated] = useState<CreatedIngestToken>();

  function reset(nextOpen: boolean) {
    setOpen(nextOpen);
    setName("");
    setDays("90");
    create.reset();
  }

  function submit(event: React.FormEvent) {
    event.preventDefault();
    create.mutate(
      { name, expires_in_days: Number(days) },
      {
        onSuccess: (token) => {
          reset(false);
          setCreated(token);
        },
      },
    );
  }

  return (
    <>
      <Dialog open={open} onOpenChange={reset}>
        <DialogTrigger asChild>
          <Button size="sm" variant="outline">
            New ingest token
          </Button>
        </DialogTrigger>
        <DialogContent>
          <form onSubmit={submit} className="flex flex-col gap-6">
            <DialogHeader>
              <DialogTitle>New ingest token for {integration.title}</DialogTitle>
              <DialogDescription>
                Name the pipeline or job that pushes, so you know what breaks when you revoke it.
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="ingest-token-name">Name</FieldLabel>
                <Input
                  id="ingest-token-name"
                  required
                  maxLength={100}
                  placeholder="deploy-pipeline"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="ingest-token-expiry">Expires in</FieldLabel>
                <Select value={days} onValueChange={setDays}>
                  <SelectTrigger id="ingest-token-expiry">
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
      <SecretDialog
        title="New ingest token"
        secret={created?.secret}
        onClose={() => setCreated(undefined)}
      >
        {created && (
          <div className="flex flex-col gap-2 text-sm">
            <p className="text-muted-foreground">Push with it, for example:</p>
            <code className="font-mono break-all whitespace-pre-wrap">
              {pushCommand(integration, created)}
            </code>
            <p className="text-muted-foreground">{payloadHint(integration)}</p>
            <div>
              <CopyButton value={pushCommand(integration, created)} label="Copy command" />
            </div>
          </div>
        )}
      </SecretDialog>
    </>
  );
}
