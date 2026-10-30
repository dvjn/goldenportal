import { useSuspenseQuery } from "@tanstack/react-query";
import {
  createFileRoute,
  type ErrorComponentProps,
  Link,
  useNavigate,
} from "@tanstack/react-router";
import { ArrowLeft, ArrowRight, Plug } from "lucide-react";
import { stringify } from "yaml";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { EntityLink } from "@/components/catalog/entity-link";
import { PropertyValue } from "@/components/catalog/property-value";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageError } from "@/components/page-error";
import {
  ScorecardChip,
  ScorecardResult,
  ScorecardTab,
} from "@/components/scorecards/entity-scorecards";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { WidgetGrid } from "@/components/widgets/widget-grid";
import { type BlueprintSpec, blueprints } from "@/features/blueprints";
import { type EntityDetail, entityQueryOptions, type Incoming } from "@/features/catalog";
import { entityScorecardsQueryOptions } from "@/features/scorecards";
import { formatAbsolute, formatRelative } from "@/lib/format";

// The blueprint's entity tabs and each scorecard that scores the entity get
// a tab, between Overview and the system tabs. A blueprint tab named
// overview takes the built-in Overview's place.
const systemTabs = ["relations", "yaml"] as const;

export const Route = createFileRoute("/_app/catalog/$blueprint/$")({
  validateSearch: (search: Record<string, unknown>): { tab?: string } =>
    typeof search.tab === "string" && search.tab !== "" ? { tab: search.tab } : {},
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.blueprint)),
      context.queryClient.ensureQueryData(
        entityQueryOptions(params.blueprint, params._splat ?? ""),
      ),
      context.queryClient.ensureQueryData(
        entityScorecardsQueryOptions(params.blueprint, params._splat ?? ""),
      ),
    ]),
  component: EntityPage,
  errorComponent: EntityError,
});

function EntityPage() {
  const { blueprint, _splat: identifier = "" } = Route.useParams();
  const { tab = "overview" } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: doc } = useSuspenseQuery(blueprints.queries.detail(blueprint));
  const { data: entity } = useSuspenseQuery(entityQueryOptions(blueprint, identifier));
  const { data: results } = useSuspenseQuery(entityScorecardsQueryOptions(blueprint, identifier));
  const spec = doc.published?.spec;
  if (!spec) return null;
  const tabs = spec.entity?.tabs ?? [];
  const overview = tabs.find((t) => t.identifier === "overview");
  const views = tabs.filter((t) => t.identifier !== "overview");
  const known: string[] = [
    "overview",
    ...views.map((v) => v.identifier),
    ...results.map((r) => r.scorecard),
    ...systemTabs,
  ];
  const active = known.includes(tab) ? tab : "overview";
  const tabClass =
    "focus-visible:border-transparent focus-visible:bg-muted focus-visible:ring-0 focus-visible:outline-none";

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-3">
        <PageBreadcrumbs page={entity.title}>
          <Link to="/catalog">Catalog</Link>
          <Link to="/catalog/$blueprint" params={{ blueprint }}>
            {spec.title}
          </Link>
        </PageBreadcrumbs>
        <div className="flex items-start gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted">
            <BlueprintIcon icon={spec.icon} className="size-5" />
          </span>
          <div className="grid min-w-0 gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-2xl font-semibold tracking-tight">{entity.title}</h1>
              <Badge variant="secondary">{spec.title}</Badge>
              {results.map((result) => (
                <ScorecardChip key={result.scorecard} result={result} />
              ))}
            </div>
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
              <code className="font-mono">{entity.identifier}</code>
              <span className="flex items-center gap-1">
                <Plug className="size-3.5" aria-hidden />
                <Link
                  to="/integrations/$identifier"
                  params={{ identifier: entity.integration }}
                  className="hover:underline"
                >
                  {entity.integration}
                </Link>
              </span>
              <span title={formatAbsolute(entity.updatedAt)}>
                Updated {formatRelative(entity.updatedAt)}
              </span>
            </p>
          </div>
        </div>
      </div>

      <Tabs
        value={active}
        onValueChange={(next) => navigate({ search: { tab: next }, replace: true })}
      >
        <TabsList variant="line" aria-label="Entity">
          <TabsTrigger value="overview" className={tabClass}>
            {overview?.title ?? "Overview"}
          </TabsTrigger>
          {views.map((v) => (
            <TabsTrigger key={v.identifier} value={v.identifier} className={tabClass}>
              {v.title}
            </TabsTrigger>
          ))}
          {results.map((result) => (
            <TabsTrigger key={result.scorecard} value={result.scorecard} className={tabClass}>
              <ScorecardTab result={result} />
            </TabsTrigger>
          ))}
          <TabsTrigger value="relations" className={tabClass}>
            Relations
            <span className="text-xs text-muted-foreground tabular-nums">
              {relationCount(spec, entity)}
            </span>
          </TabsTrigger>
          <TabsTrigger value="yaml" className={tabClass}>
            YAML
          </TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="pt-2">
          {overview ? (
            <WidgetGrid
              widgets={overview.widgets}
              source={{ blueprint, page: "entity", tab: overview.identifier }}
              context={{ entity: identifier, entityData: entity }}
            />
          ) : (
            <Overview spec={spec} entity={entity} />
          )}
        </TabsContent>
        {views.map((v) => (
          <TabsContent key={v.identifier} value={v.identifier} className="pt-2">
            <WidgetGrid
              widgets={v.widgets}
              source={{ blueprint, page: "entity", tab: v.identifier }}
              context={{ entity: identifier, entityData: entity }}
            />
          </TabsContent>
        ))}
        {results.map((result) => (
          <TabsContent key={result.scorecard} value={result.scorecard} className="pt-2">
            <ScorecardResult result={result} />
          </TabsContent>
        ))}
        <TabsContent value="relations" className="pt-2">
          <Relations spec={spec} entity={entity} />
        </TabsContent>
        <TabsContent value="yaml" className="pt-2">
          <Card className="py-0">
            <pre className="overflow-x-auto p-4 font-mono text-xs leading-5">
              {stringify({
                blueprint: entity.blueprint,
                identifier: entity.identifier,
                title: entity.title,
                properties: entity.properties,
                integration: entity.integration,
                createdAt: entity.createdAt,
                updatedAt: entity.updatedAt,
              })}
            </pre>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function relationCount(spec: BlueprintSpec, entity: EntityDetail) {
  const outgoing = spec.properties.reduce(
    (sum, property) => sum + (entity.related[property.identifier]?.length ?? 0),
    0,
  );
  return outgoing + entity.incoming.reduce((sum, incoming) => sum + incoming.total, 0);
}

function Overview({ spec, entity }: Readonly<{ spec: BlueprintSpec; entity: EntityDetail }>) {
  const shown = spec.properties.filter((property) => !property.display?.hidden);
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <Card>
        <CardHeader className="border-b">
          <CardTitle>Properties</CardTitle>
        </CardHeader>
        <CardContent>
          {shown.length === 0 ? (
            <p className="text-sm text-muted-foreground">This blueprint has no properties.</p>
          ) : (
            <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[minmax(8rem,auto)_minmax(0,1fr)]">
              {shown.map((property) => (
                <div key={property.identifier} className="contents">
                  <dt className="text-muted-foreground" title={property.description}>
                    {property.title}
                  </dt>
                  <dd className="min-w-0">
                    <PropertyValue
                      property={property}
                      value={entity.properties[property.identifier]}
                      links={entity.related[property.identifier]}
                    />
                  </dd>
                </div>
              ))}
            </dl>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="border-b">
          <CardTitle>Pointed At By</CardTitle>
        </CardHeader>
        <CardContent>
          {entity.incoming.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing relates to this entity.</p>
          ) : (
            <ul className="grid gap-2 text-sm">
              {entity.incoming.map((incoming) => (
                <li
                  key={`${incoming.blueprint}.${incoming.property}`}
                  className="flex items-center justify-between gap-3"
                >
                  <span className="truncate">
                    {incoming.blueprintTitle}{" "}
                    <span className="text-muted-foreground">via {incoming.propertyTitle}</span>
                  </span>
                  <IncomingLink incoming={incoming} identifier={entity.identifier}>
                    {incoming.total}
                  </IncomingLink>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// IncomingLink opens the other blueprint's catalog, filtered to the entities
// that point at this one.
function IncomingLink({
  incoming,
  identifier,
  children,
}: Readonly<{ incoming: Incoming; identifier: string; children: React.ReactNode }>) {
  return (
    <Link
      to="/catalog/$blueprint"
      params={{ blueprint: incoming.blueprint }}
      search={{ f: { [incoming.property]: [identifier] } }}
      className="font-medium tabular-nums hover:underline"
    >
      {children}
    </Link>
  );
}

function Relations({ spec, entity }: Readonly<{ spec: BlueprintSpec; entity: EntityDetail }>) {
  const outgoing = spec.properties.filter((property) => property.relation);

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2">
            <ArrowRight className="size-4 text-muted-foreground" aria-hidden />
            Points To
          </CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4">
          {outgoing.length === 0 ? (
            <p className="text-sm text-muted-foreground">{spec.title} has no relations.</p>
          ) : (
            outgoing.map((property) => (
              <section key={property.identifier} className="grid gap-1.5">
                <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                  {property.title}
                </h3>
                <PropertyValue
                  property={property}
                  value={entity.properties[property.identifier]}
                  links={entity.related[property.identifier]}
                />
              </section>
            ))
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2">
            <ArrowLeft className="size-4 text-muted-foreground" aria-hidden />
            Pointed At By
          </CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4">
          {entity.incoming.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing relates to this entity.</p>
          ) : (
            entity.incoming.map((incoming) => (
              <section key={`${incoming.blueprint}.${incoming.property}`} className="grid gap-1.5">
                <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                  {incoming.blueprintTitle} via {incoming.propertyTitle}
                </h3>
                <span className="flex flex-wrap gap-x-2 gap-y-1 text-sm">
                  {incoming.entities.map((link) => (
                    <EntityLink
                      key={link.identifier}
                      blueprint={incoming.blueprint}
                      identifier={link.identifier}
                    >
                      {link.title || link.identifier}
                    </EntityLink>
                  ))}
                </span>
                {incoming.total > incoming.entities.length && (
                  <IncomingLink incoming={incoming} identifier={entity.identifier}>
                    See all {incoming.total}
                  </IncomingLink>
                )}
              </section>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function EntityError(props: ErrorComponentProps) {
  const { blueprint } = Route.useParams();
  return (
    <PageError
      {...props}
      back={
        <Link to="/catalog/$blueprint" params={{ blueprint }}>
          Back to the list
        </Link>
      }
    />
  );
}
