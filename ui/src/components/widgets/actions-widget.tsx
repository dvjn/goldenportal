import { useQuery } from "@tanstack/react-query";
import { Zap } from "lucide-react";
import { useState } from "react";
import { RunActionDialog } from "@/components/actions/run-action-dialog";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { EmptyState } from "@/components/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  availableQueryOptions,
  type Offer,
  selfServiceCatalogQueryOptions,
} from "@/features/actions";
import { entityQueryOptions } from "@/features/catalog";
import { WidgetCard, WidgetLoading, type WidgetProps } from "./widget-grid";

// ActionsWidget offers the actions and then the workflows it lists, in its
// order, leaving out those the viewer can't run. On an entity's page its
// actions run on that entity, and only those the viewer can run there
// show. Elsewhere each action's form asks for the entity first, and the
// server says on submit whether the viewer may run it there.
export function ActionsWidget(props: WidgetProps) {
  const { source, context } = props;
  if ("blueprint" in source && source.page === "entity" && context.entity) {
    return <EntityActions {...props} blueprint={source.blueprint} entity={context.entity} />;
  }
  return <CatalogActions {...props} />;
}

// pick finds what the widget lists in the Self-Service catalog, which has
// only what the viewer can run.
function pick(items: Offer[], kind: Offer["kind"], ids: string[] = []) {
  return ids.flatMap((id) => items.filter((o) => o.kind === kind && o.identifier === id));
}

function EntityActions({
  widget,
  blueprint,
  entity,
}: WidgetProps & { blueprint: string; entity: string }) {
  const available = useQuery(availableQueryOptions(blueprint, entity));
  const catalog = useQuery({ ...selfServiceCatalogQueryOptions, enabled: !!widget.workflows });
  const { data: detail } = useQuery(entityQueryOptions(blueprint, entity));
  if (available.isPending || (widget.workflows && catalog.isPending)) {
    return <WidgetLoading widget={widget} />;
  }
  const actions = (widget.actions ?? []).flatMap((id): Offer[] => {
    const action = available.data?.find((a) => a.identifier === id);
    return action ? [{ ...action, kind: "action", blueprint }] : [];
  });
  const items = catalog.data?.groups.flatMap((group) => group.items) ?? [];
  return (
    <OfferList
      widget={widget}
      offers={[...actions, ...pick(items, "workflow", widget.workflows)]}
      entity={{ blueprint, identifier: entity, title: detail?.title ?? entity }}
    />
  );
}

function CatalogActions({ widget }: WidgetProps) {
  const catalog = useQuery(selfServiceCatalogQueryOptions);
  if (catalog.isPending) return <WidgetLoading widget={widget} />;
  const items = catalog.data?.groups.flatMap((group) => group.items) ?? [];
  return (
    <OfferList
      widget={widget}
      offers={[
        ...pick(items, "action", widget.actions),
        ...pick(items, "workflow", widget.workflows),
      ]}
    />
  );
}

function OfferList({
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
          description={entity ? "None of these can be run here." : "None of these can be run."}
        />
      </WidgetCard>
    );
  }
  return (
    <WidgetCard widget={widget}>
      <ul className="grid gap-2">
        {offers.map((offer) => (
          <li key={`${offer.kind}:${offer.identifier}`}>
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
          kind={picked.kind}
          entity={picked.kind === "action" ? entity : undefined}
          blueprint={picked.blueprint}
          open={open}
          onOpenChange={setOpen}
        />
      )}
    </WidgetCard>
  );
}
