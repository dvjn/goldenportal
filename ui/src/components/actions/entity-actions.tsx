import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ChevronDown, Zap } from "lucide-react";
import { type ReactNode, useState } from "react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { type Available, availableQueryOptions, rowActionsQueryOptions } from "@/features/actions";
import { RunActionDialog, type RunTarget } from "./run-action-dialog";

// ActionsMenu lists actions to run on an entity, destructive ones marked as
// such, each opening its form. trigger is what opens the menu.
function ActionsMenu({
  entity,
  actions,
  trigger,
}: Readonly<{ entity: RunTarget; actions: Available[]; trigger: ReactNode }>) {
  const [picked, setPicked] = useState<Available>();
  const [open, setOpen] = useState(false);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-48">
          {actions.map((action) => (
            <DropdownMenuItem
              key={action.identifier}
              variant={action.destructive ? "destructive" : "default"}
              onSelect={() => {
                setPicked(action);
                setOpen(true);
              }}
            >
              <BlueprintIcon icon={action.icon} />
              {action.title}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      {picked && (
        <RunActionDialog action={picked} entity={entity} open={open} onOpenChange={setOpen} />
      )}
    </>
  );
}

// EntityActions offers the actions the signed-in user can run on an
// entity, each opening its form. It shows nothing when there are none.
export function EntityActions({ entity }: Readonly<{ entity: RunTarget }>) {
  const { data: available = [] } = useQuery(
    availableQueryOptions(entity.blueprint, entity.identifier),
  );
  if (available.length === 0) return null;

  return (
    <ActionsMenu
      entity={entity}
      actions={available}
      trigger={
        <Button variant="outline">
          <Zap />
          Actions
          <ChevronDown className="text-muted-foreground" />
        </Button>
      }
    />
  );
}

// useRowActions asks once for a page of a blueprint's rows which actions
// each one offers, and returns them by row. live is false where rows can't
// be acted on, such as a builder's preview.
export function useRowActions(blueprint: string, identifiers: string[], live = true) {
  const { data } = useQuery({
    ...rowActionsQueryOptions(blueprint, identifiers),
    enabled: live && identifiers.length > 0,
    placeholderData: keepPreviousData,
  });
  return (identifier: string): Available[] =>
    (data?.entities[identifier] ?? []).flatMap(
      (id) => data?.actions.find((action) => action.identifier === id) ?? [],
    );
}

// RowActions is a table row's menu of the actions its entity offers. A row
// with none has no menu.
export function RowActions({
  entity,
  actions,
}: Readonly<{ entity: RunTarget; actions: Available[] }>) {
  if (actions.length === 0) return null;
  return (
    <ActionsMenu
      entity={entity}
      actions={actions}
      trigger={
        <IconButton label={`Actions for ${entity.title}`}>
          <Zap />
        </IconButton>
      }
    />
  );
}
