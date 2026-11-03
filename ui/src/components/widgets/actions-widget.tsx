import { useQueries, useQuery } from "@tanstack/react-query";
import { Zap } from "lucide-react";
import { useState } from "react";
import { RunActionDialog } from "@/components/actions/run-action-dialog";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { EmptyState } from "@/components/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { type Available, actions, availableQueryOptions } from "@/features/actions";
import { entityQueryOptions } from "@/features/catalog";
import { WidgetCard, WidgetLoading, type WidgetProps } from "./widget-grid";

// Offer is an action the widget can open, with the blueprint whose
// entities it runs on.
type Offer = Available & { blueprint: string };

// ActionsWidget offers the actions it lists, in its order. On an entity's
// page they run on that entity, and only those the viewer can run there
// show. Elsewhere each one's form asks for the entity first, and the
// server says on submit whether the viewer may run it there.
export function ActionsWidget(props: WidgetProps) {
  const { source, context } = props;
  if ("blueprint" in source && source.page === "entity" && context.entity) {
    return <EntityActions {...props} blueprint={source.blueprint} entity={context.entity} />;
  }
  return <PickedActions {...props} />;
}

function EntityActions({
  widget,
  blueprint,
  entity,
}: WidgetProps & { blueprint: string; entity: string }) {
  const available = useQuery(availableQueryOptions(blueprint, entity));
  const { data: detail } = useQuery(entityQueryOptions(blueprint, entity));
  if (available.isPending) return <WidgetLoading widget={widget} />;
  const offers = (widget.actions ?? []).flatMap((id) => {
    const action = available.data?.find((a) => a.identifier === id);
    return action ? [{ ...action, blueprint }] : [];
  });
  return (
    <ActionList
      widget={widget}
      offers={offers}
      entity={{ blueprint, identifier: entity, title: detail?.title ?? entity }}
    />
  );
}

// PickedActions reads each action's published definition, which says the
// blueprint to pick from; actions that aren't published, or that the
// viewer can't read, are left out.
function PickedActions({ widget }: WidgetProps) {
  const ids = widget.actions ?? [];
  const docs = useQueries({
    queries: ids.map((id) => ({ ...actions.queries.detail(id), retry: false })),
  });
  if (docs.some((doc) => doc.isPending)) return <WidgetLoading widget={widget} />;
  const offers = docs.flatMap((doc): Offer[] => {
    const spec = doc.data?.published?.spec;
    if (!spec) return [];
    return [
      {
        identifier: spec.identifier,
        title: spec.title,
        description: spec.description,
        icon: spec.icon,
        color: spec.color,
        destructive: spec.destructive,
        inputs: spec.inputs,
        blueprint: spec.blueprint,
      },
    ];
  });
  return <ActionList widget={widget} offers={offers} />;
}

function ActionList({
  widget,
  offers,
  entity,
}: Readonly<{
  widget: WidgetProps["widget"];
  offers: Offer[];
  entity?: { blueprint: string; identifier: string; title: string };
}>) {
  const [picked, setPicked] = useState<Offer>();
  const [open, setOpen] = useState(false);

  if (offers.length === 0) {
    return (
      <WidgetCard widget={widget}>
        <EmptyState
          icon={<Zap />}
          title="Nothing to run"
          description={
            entity ? "None of these actions can be run here." : "None of these actions can be run."
          }
        />
      </WidgetCard>
    );
  }
  return (
    <WidgetCard widget={widget}>
      <ul className="grid gap-2">
        {offers.map((offer) => (
          <li key={offer.identifier}>
            <Button
              variant="outline"
              className="h-auto w-full justify-start py-2"
              onClick={() => {
                setPicked(offer);
                setOpen(true);
              }}
            >
              <IconTile icon={offer.icon} color={offer.color} size="sm" />
              <span className="truncate">{offer.title}</span>
              {offer.destructive && (
                <Badge variant="destructive" className="ml-auto">
                  Destructive
                </Badge>
              )}
            </Button>
          </li>
        ))}
      </ul>
      {picked && (
        <RunActionDialog
          action={picked}
          entity={entity}
          blueprint={picked.blueprint}
          open={open}
          onOpenChange={setOpen}
        />
      )}
    </WidgetCard>
  );
}
