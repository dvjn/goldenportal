import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
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
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { ConfigDocs } from "@/features/config-docs";

type DeleteDocDialogProps<S> = {
  docs: ConfigDocs<S>;
  identifier: string;
  // noun names the kind in the confirm button, e.g. "Blueprint".
  noun: string;
  description: string;
  onDeleted: () => Promise<void> | void;
};

// DeleteDocDialog deletes a document once its identifier is typed out.
export function DeleteDocDialog<S>({
  docs,
  identifier,
  noun,
  description,
  onDeleted,
}: DeleteDocDialogProps<S>) {
  const [typed, setTyped] = useState("");
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: () => docs.remove(identifier),
    onSuccess: async () => {
      queryClient.removeQueries({ queryKey: docs.keys.detail(identifier) });
      await queryClient.invalidateQueries({ queryKey: docs.keys.all });
      toast.success(`Deleted ${identifier}`);
      await onDeleted();
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <AlertDialog onOpenChange={() => setTyped("")}>
      <AlertDialogTrigger asChild>
        <Button variant="outline">
          <Trash2 />
          Delete
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {identifier}?</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <Field>
          <FieldLabel htmlFor="confirm-identifier">
            Type <code className="font-mono">{identifier}</code> to confirm
          </FieldLabel>
          <Input
            id="confirm-identifier"
            value={typed}
            onChange={(event) => setTyped(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={typed !== identifier || remove.isPending}
            onClick={() => remove.mutate()}
          >
            Delete {noun}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
