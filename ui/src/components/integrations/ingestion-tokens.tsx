import { useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { Check, Copy, Plus, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { toast } from "sonner";
import { ConfirmIconButton } from "@/components/confirm-icon-button";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  type CreatedIngestionToken,
  createToken,
  integrationKeys,
  revokeToken,
  tokensQueryOptions,
} from "@/features/integrations";
import { formatAbsolute, formatRelative } from "@/lib/format";

// IngestionTokens lists an integration's tokens, which push entities as it.
export function IngestionTokens({ identifier }: Readonly<{ identifier: string }>) {
  const { data: tokens } = useSuspenseQuery(tokensQueryOptions(identifier));
  const queryClient = useQueryClient();
  const revoke = useMutation({
    mutationFn: (id: string) => revokeToken(identifier, id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: integrationKeys.tokens(identifier) });
      toast.success("Revoked the token");
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <div className="grid gap-3">
      {tokens.length === 0 ? (
        <p className="text-sm text-muted-foreground">No tokens yet, so nothing can push.</p>
      ) : (
        <ul className="grid gap-2">
          {tokens.map((token) => (
            <li key={token.id} className="flex items-center justify-between gap-3 text-sm">
              <div className="grid min-w-0">
                <span className="truncate font-medium">{token.name}</span>
                <span className="truncate text-xs text-muted-foreground">
                  <code className="font-mono">{token.prefix}…</code> ·{" "}
                  {token.lastUsedAt ? (
                    <span title={formatAbsolute(token.lastUsedAt)}>
                      used {formatRelative(token.lastUsedAt)}
                    </span>
                  ) : (
                    "never used"
                  )}
                </span>
              </div>
              <ConfirmIconButton
                label={`Revoke ${token.name}`}
                icon={<Trash2 />}
                title={`Revoke ${token.name}?`}
                description="Anything pushing with it stops working straight away."
                confirm="Revoke Token"
                onConfirm={() => revoke.mutate(token.id)}
              />
            </li>
          ))}
        </ul>
      )}
      <CreateIngestionTokenDialog identifier={identifier} />
    </div>
  );
}

function CreateIngestionTokenDialog({ identifier }: Readonly<{ identifier: string }>) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [created, setCreated] = useState<CreatedIngestionToken | null>(null);
  const queryClient = useQueryClient();
  const create = useMutation({
    mutationFn: () => createToken(identifier, name.trim()),
    onSuccess: async (token) => {
      setCreated(token);
      await queryClient.invalidateQueries({ queryKey: integrationKeys.tokens(identifier) });
    },
    onError: (error) => toast.error(error.message),
  });

  function onOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      setName("");
      setCreated(null);
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" className="justify-self-start">
          <Plus />
          New Token
        </Button>
      </DialogTrigger>
      <DialogContent>
        {created ? (
          <TokenSecret created={created} onDone={() => onOpenChange(false)} />
        ) : (
          <form onSubmit={submit} className="grid gap-6">
            <DialogHeader>
              <DialogTitle>New Ingestion Token</DialogTitle>
              <DialogDescription>
                It can push and delete entities as {identifier}, and nothing else.
              </DialogDescription>
            </DialogHeader>
            <Field>
              <FieldLabel htmlFor="ingestion-token-name">Name</FieldLabel>
              <Input
                id="ingestion-token-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="CI pipeline"
                maxLength={100}
                autoComplete="off"
              />
            </Field>
            <DialogFooter>
              <Button type="submit" disabled={!name.trim() || create.isPending}>
                Create Token
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function TokenSecret({
  created,
  onDone,
}: Readonly<{ created: CreatedIngestionToken; onDone: () => void }>) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    await navigator.clipboard.writeText(created.secret);
    setCopied(true);
  }

  return (
    <div className="grid gap-6">
      <DialogHeader>
        <DialogTitle>Copy your new token</DialogTitle>
        <DialogDescription>
          This is the only time "{created.name}" is shown. Store it with the system that pushes,
          which sends it as <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>.
        </DialogDescription>
      </DialogHeader>
      <Field orientation="horizontal">
        <Input
          readOnly
          value={created.secret}
          aria-label="New ingestion token"
          className="font-mono"
          onFocus={(event) => event.currentTarget.select()}
        />
        <Button type="button" variant="outline" onClick={copy}>
          {copied ? <Check /> : <Copy />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </Field>
      <DialogFooter>
        <Button type="button" onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </div>
  );
}
