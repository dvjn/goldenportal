import { createFileRoute, Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  BlueprintIcon,
  type EntityName,
  IntegrationBadge,
  PropertyValue,
  useBlueprint,
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
