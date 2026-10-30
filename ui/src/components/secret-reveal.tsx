import type { ReactNode } from "react";
import { CopyButton } from "@/components/copy-button";
import { Button } from "@/components/ui/button";
import { DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

type SecretRevealProps = {
  secret: string;
  // label names the secret for screen readers, e.g. "New API token".
  label: string;
  // children say where the secret goes and how it's sent.
  children: ReactNode;
  onDone: () => void;
};

// SecretReveal shows a new secret in its dialog, the one time it can be read.
export function SecretReveal({ secret, label, children, onDone }: SecretRevealProps) {
  return (
    <div className="grid gap-6">
      <DialogHeader>
        <DialogTitle>Copy your new token</DialogTitle>
        <DialogDescription>{children}</DialogDescription>
      </DialogHeader>
      <Field orientation="horizontal">
        <Input
          readOnly
          value={secret}
          aria-label={label}
          className="font-mono"
          onFocus={(event) => event.currentTarget.select()}
        />
        <CopyButton value={secret} />
      </Field>
      <DialogFooter>
        <Button type="button" onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </div>
  );
}
