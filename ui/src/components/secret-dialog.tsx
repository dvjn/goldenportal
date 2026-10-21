import type { ReactNode } from "react";
import { CopyButton } from "@/components/copy-button";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

type SecretDialogProps = {
  title: string;
  secret: string | undefined;
  onClose: () => void;
  children?: ReactNode;
};

export function SecretDialog({ title, secret, onClose, children }: SecretDialogProps) {
  return (
    <Dialog open={secret !== undefined} onOpenChange={(open) => open || onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>Copy it now. You won't be able to see it again.</DialogDescription>
        </DialogHeader>
        <Input readOnly value={secret ?? ""} aria-label={title} className="font-mono" />
        {children}
        <DialogFooter>
          <CopyButton value={secret ?? ""} label="Copy" />
          <DialogClose asChild>
            <Button>Done</Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
