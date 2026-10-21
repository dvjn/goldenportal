import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
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
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useCan } from "@/features/auth";
import {
  BlueprintIcon,
  type Entity,
  type EntityName,
  IntegrationBadge,
  PropertyValue,
  useBlueprint,
  useDeleteEntity,
  useEntity,
} from "@/features/catalog";
import { formatDateTime, formatRelative } from "@/lib/time";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/$identifier/")({
  component: EntityPage,
});

function EntityPage() {
  const params = Route.useParams();
  const blueprint = useBlueprint(params.blueprint);
  const entity = useEntity(params.blueprint, params.identifier);

  return (
    <>
      <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            <BlueprintIcon icon={blueprint.icon} className="size-5" />
            {entity.title}
          </h1>
          <p className="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">
            <span>{blueprint.title}</span>
            <span className="font-mono text-xs">{entity.identifier}</span>
            <IntegrationBadge integration={entity.integration} />
          </p>
        </div>
        <EntityActions entity={entity} />
      </header>
      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Properties</CardTitle>
          </CardHeader>
          <CardContent>
            {blueprint.properties.length === 0 ? (
              <p className="text-muted-foreground text-sm">This blueprint has no properties.</p>
            ) : (
              <dl className="grid gap-x-4 gap-y-3 text-sm sm:grid-cols-[minmax(8rem,auto)_1fr]">
                {blueprint.properties.map((property) => (
                  <div key={property.key} className="contents">
                    <dt className="text-muted-foreground">{property.title}</dt>
                    <dd>
                      <PropertyValue property={property} value={entity.properties[property.key]} />
                    </dd>
                  </div>
                ))}
              </dl>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Relations</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4 text-sm">
            {entity.relations.map((relation) => (
              <section key={relation.key} className="flex flex-col gap-1">
                <h2 className="text-muted-foreground">{relation.title}</h2>
                <EntityLinks entities={relation.entities} />
              </section>
            ))}
            {entity.incoming.map((group) => (
              <section key={`${group.source}.${group.relation}`} className="flex flex-col gap-1">
                <h2 className="text-muted-foreground">{group.title}</h2>
                <EntityLinks entities={group.entities} />
              </section>
            ))}
            {entity.relations.length === 0 && entity.incoming.length === 0 && (
              <p className="text-muted-foreground">No relations.</p>
            )}
            <dl className="text-muted-foreground grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 border-t pt-4">
              <dt>Created</dt>
              <dd title={formatDateTime(entity.created_at)}>{formatRelative(entity.created_at)}</dd>
              <dt>Updated</dt>
              <dd title={formatDateTime(entity.updated_at)}>{formatRelative(entity.updated_at)}</dd>
            </dl>
          </CardContent>
        </Card>
      </div>
    </>
  );
}

function EntityLinks({ entities }: { entities: EntityName[] }) {
  if (entities.length === 0) return <span className="text-muted-foreground">None</span>;

  return (
    <ul className="flex flex-col gap-1">
      {entities.map((entity) => (
        <li key={`${entity.blueprint}/${entity.identifier}`}>
          <Link
            to="/catalog/$blueprint/$identifier"
            params={{ blueprint: entity.blueprint, identifier: entity.identifier }}
            className="underline-offset-4 hover:underline"
          >
            {entity.title}
          </Link>
        </li>
      ))}
    </ul>
  );
}

function EntityActions({ entity }: { entity: Entity }) {
  const can = useCan();

  if (!can("catalog.write")) return null;
  if (!entity.integration.manual) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="flex gap-2">
            <Button variant="outline" disabled>
              Edit
            </Button>
            <Button variant="outline" disabled>
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
          Edit
        </Link>
      </Button>
      <DeleteEntity entity={entity} />
    </div>
  );
}

function DeleteEntity({ entity }: { entity: Entity }) {
  const remove = useDeleteEntity(entity.blueprint);
  const navigate = useNavigate();

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline">Delete</Button>
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
