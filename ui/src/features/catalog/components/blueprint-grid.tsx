import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { StateCard } from "@/components/common/state-card";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useCan } from "@/features/auth";
import type { Blueprint } from "../api";
import { entityCountLabel } from "../format";
import { blueprintsQueryOptions, useSourceIcons } from "../queries";
import { BlueprintIconTile } from "./blueprint-icon";
import { SourceMark } from "./source-mark";

const skeletonIds = ["card-1", "card-2", "card-3", "card-4", "card-5", "card-6"];

function linkedTitles(blueprint: Blueprint, blueprints: Blueprint[]): string[] {
  const targets = new Set(blueprint.relations.map((relation) => relation.target));
  return [...targets].map(
    (target) => blueprints.find((candidate) => candidate.identifier === target)?.title ?? target,
  );
}

function BlueprintCard({
  blueprint,
  blueprints,
  sourceIcon,
}: Readonly<{ blueprint: Blueprint; blueprints: Blueprint[]; sourceIcon: string }>) {
  const links = linkedTitles(blueprint, blueprints);
  return (
    <Link
      to="/catalog/$blueprint"
      params={{ blueprint: blueprint.identifier }}
      className="row-span-4 grid grid-rows-subgrid rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      <Card className="row-span-4 grid grid-rows-subgrid gap-y-2 transition-colors hover:bg-muted/50">
        <CardHeader className="mb-2 flex items-center gap-3">
          <BlueprintIconTile icon={blueprint.icon} />
          <CardTitle className="line-clamp-2 min-w-0 flex-1 text-base">{blueprint.title}</CardTitle>
          <SourceMark icon={sourceIcon} className="size-4 shrink-0" />
        </CardHeader>
        <p className="px-4 text-sm text-muted-foreground">{blueprint.description}</p>
        <p className="flex items-baseline gap-1.5 px-4">
          <span className="text-2xl font-semibold tabular-nums">
            {blueprint.entity_count.toLocaleString("en")}
          </span>
          <span className="text-sm text-muted-foreground">
            {entityCountLabel(blueprint.entity_count)}
          </span>
        </p>
        <p className="px-4 text-sm text-muted-foreground">
          {links.length > 0 ? `Links to ${links.join(", ")}` : null}
        </p>
      </Card>
    </Link>
  );
}

export function BlueprintGrid() {
  const blueprints = useQuery(blueprintsQueryOptions);
  const sources = useSourceIcons();
  const can = useCan();

  if (blueprints.isError) {
    return <StateCard title="Catalog unavailable" message="We could not load the blueprints." />;
  }
  if (!blueprints.data) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {skeletonIds.map((id) => (
          <Skeleton key={id} className="h-44 rounded-xl" />
        ))}
      </div>
    );
  }
  if (blueprints.data.length === 0) {
    return (
      <StateCard
        title="The catalog is empty"
        message="Add a blueprint to the definitions to start cataloging entities."
      >
        {can("catalog.model") ? (
          <Button asChild variant="outline" size="sm">
            <Link to="/settings/catalog-setup" search={{ tab: "definitions" }}>
              Open the definitions
            </Link>
          </Button>
        ) : null}
      </StateCard>
    );
  }

  const all = blueprints.data;
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {all.map((blueprint) => (
        <BlueprintCard
          key={blueprint.identifier}
          blueprint={blueprint}
          blueprints={all}
          sourceIcon={sources.data?.ofBlueprint(blueprint.identifier) ?? ""}
        />
      ))}
    </div>
  );
}
