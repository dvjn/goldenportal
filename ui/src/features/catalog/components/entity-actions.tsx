import { Link, useNavigate } from "@tanstack/react-router";
import { PencilIcon, Trash2Icon } from "lucide-react";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useCan } from "@/features/auth";
import type { Entity } from "../api";
import { useDeleteEntity } from "../queries";

export function EntityActions({ entity }: Readonly<{ entity: Entity }>) {
  const can = useCan();

  if (!can("catalog.write")) return null;
  if (!entity.integration.manual) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="flex gap-2">
            <Button variant="outline" disabled>
              <PencilIcon aria-hidden="true" />
              Edit
            </Button>
            <Button variant="outline" disabled>
              <Trash2Icon aria-hidden="true" />
              Delete
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent>Managed by the {entity.integration.title} integration</TooltipContent>
      </Tooltip>
    );
  }

  return (
    <div className="flex gap-2">
      <Button variant="outline" asChild>
        <Link
          to="/catalog/$blueprint/$identifier/edit"
          params={{ blueprint: entity.blueprint, identifier: entity.identifier }}
        >
          <PencilIcon aria-hidden="true" />
          Edit
        </Link>
      </Button>
      <DeleteEntity entity={entity} />
    </div>
  );
}

function DeleteEntity({ entity }: Readonly<{ entity: Entity }>) {
  const remove = useDeleteEntity(entity.blueprint);
  const navigate = useNavigate();

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline">
          <Trash2Icon aria-hidden="true" />
          Delete
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {entity.title}?</AlertDialogTitle>
          <AlertDialogDescription>
            The entity and its links to and from other entities go away. This cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {remove.isError && <p className="text-destructive text-sm">{remove.error.message}</p>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              event.preventDefault();
              remove.mutate(entity.identifier, {
                onSuccess: () =>
                  navigate({ to: "/catalog/$blueprint", params: { blueprint: entity.blueprint } }),
              });
            }}
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
