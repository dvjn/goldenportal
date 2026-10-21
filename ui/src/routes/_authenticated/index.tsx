import { createFileRoute, Link } from "@tanstack/react-router";
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useCan } from "@/features/auth";
import { BlueprintIcon, blueprintsQueryOptions, useBlueprints } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/")({
  loader: ({ context }) => context.queryClient.ensureQueryData(blueprintsQueryOptions),
  component: IndexPage,
});

function IndexPage() {
  const blueprints = useBlueprints();
  const can = useCan();

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">Catalog</h1>
        <p className="text-muted-foreground text-sm">
          Everything the catalog tracks, by blueprint.
        </p>
      </header>
      {blueprints.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No blueprints are defined yet.
          {can("catalog.model") && (
            <>
              {" "}
              <Link to="/definitions" className="underline">
                Define the first one
              </Link>
              .
            </>
          )}
        </p>
      ) : (
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {blueprints.map((blueprint) => (
            <li key={blueprint.identifier}>
              <Link
                to="/catalog/$blueprint"
                params={{ blueprint: blueprint.identifier }}
                className="block h-full"
              >
                <Card className="h-full">
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <BlueprintIcon icon={blueprint.icon} className="size-4" />
                      {blueprint.title}
                    </CardTitle>
                    {blueprint.description && (
                      <CardDescription>{blueprint.description}</CardDescription>
                    )}
                    <CardAction className="text-muted-foreground text-sm tabular-nums">
                      {entityCount(blueprint.entity_count)}
                    </CardAction>
                  </CardHeader>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

function entityCount(count: number) {
  return count === 1 ? "1 entity" : `${count.toLocaleString("en")} entities`;
}
