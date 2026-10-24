import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import { IconButton } from "@/components/icon-button";
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
import { type ApiToken, revokeToken, tokenKeys } from "@/features/tokens";

export function RevokeTokenButton({ token }: Readonly<{ token: ApiToken }>) {
  const queryClient = useQueryClient();
  const revoke = useMutation({
    mutationFn: () => revokeToken(token.id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: tokenKeys.all });
      toast.success(`Revoked ${token.name}`);
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <IconButton label={`Revoke ${token.name}`}>
          <Trash2 />
        </IconButton>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke "{token.name}"?</AlertDialogTitle>
          <AlertDialogDescription>
            Anything using this token stops working right away. This can't be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => revoke.mutate()}>
            Revoke token
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
