import type { ReactNode } from "react";
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

type ConfirmIconButtonProps = {
  // label names the button, e.g. "Remove Ada's access".
  label: string;
  icon: ReactNode;
  title: string;
  description: ReactNode;
  // confirm is the destructive button's text, e.g. "Remove access".
  confirm: string;
  onConfirm: () => void;
  disabled?: boolean;
};

// ConfirmIconButton is an icon button whose action needs confirming first.
export function ConfirmIconButton({
  label,
  icon,
  title,
  description,
  confirm,
  onConfirm,
  disabled,
}: ConfirmIconButtonProps) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <IconButton label={label} disabled={disabled}>
          {icon}
        </IconButton>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onConfirm}>
            {confirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
