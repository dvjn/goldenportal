import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Plus } from "lucide-react";
import { useState } from "react";
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { type CreatedToken, createToken, tokenKeys } from "@/features/tokens";

const expiryOptions = [7, 30, 90, 180, 365];

export function CreateTokenDialog() {
  const [open, setOpen] = useState(false);
  const [created, setCreated] = useState<CreatedToken>();
  const queryClient = useQueryClient();
  const create = useMutation({
    mutationFn: createToken,
    onSuccess: async (result) => {
      setCreated(result);
      await queryClient.invalidateQueries({ queryKey: tokenKeys.all });
    },
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      setCreated(undefined);
      create.reset();
    }
  }

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    create.mutate({
      name: String(form.get("name")),
      readOnly: form.get("access") === "read",
      expiresInDays: Number(form.get("expiresInDays")),
    });
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button>
          <Plus />
          New token
        </Button>
      </DialogTrigger>
      <DialogContent>
        {created ? (
          <TokenSecret created={created} onDone={() => handleOpenChange(false)} />
        ) : (
          <form onSubmit={handleSubmit} className="grid gap-6">
            <DialogHeader>
              <DialogTitle>New API token</DialogTitle>
              <DialogDescription>
                The token acts as you, with your current role, until it expires or you revoke it.
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="token-name">Name</FieldLabel>
                <Input
                  id="token-name"
                  name="name"
                  placeholder="CI pipeline"
                  maxLength={100}
                  required
                />
                <FieldDescription>
                  Something that tells you where the token is used.
                </FieldDescription>
              </Field>
              <FieldSet>
                <FieldLegend variant="label">Access</FieldLegend>
                <RadioGroup name="access" defaultValue="write">
                  <Field orientation="horizontal">
                    <RadioGroupItem value="write" id="access-write" />
                    <FieldLabel htmlFor="access-write" className="font-normal">
                      Read and write
                    </FieldLabel>
                  </Field>
                  <Field orientation="horizontal">
                    <RadioGroupItem value="read" id="access-read" />
                    <FieldLabel htmlFor="access-read" className="font-normal">
                      Read only
                    </FieldLabel>
                  </Field>
                </RadioGroup>
              </FieldSet>
              <Field>
                <FieldLabel htmlFor="token-expiry">Expires after</FieldLabel>
                <Select name="expiresInDays" defaultValue="90">
                  <SelectTrigger id="token-expiry" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {expiryOptions.map((days) => (
                      <SelectItem key={days} value={String(days)}>
                        {days} days
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <FieldError>{create.error?.message}</FieldError>
            </FieldGroup>
            <DialogFooter>
              <Button type="submit" disabled={create.isPending}>
                Create token
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

function TokenSecret({ created, onDone }: Readonly<{ created: CreatedToken; onDone: () => void }>) {
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
          This is the only time "{created.token.name}" is shown. Store it somewhere safe, then send
          it as <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>.
        </DialogDescription>
      </DialogHeader>
      <Field orientation="horizontal">
        <Input
          readOnly
          value={created.secret}
          aria-label="New API token"
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
