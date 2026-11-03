import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import {
  createFileRoute,
  type ErrorComponentProps,
  Link,
  useNavigate,
} from "@tanstack/react-router";
import { ArrowLeft, ExternalLink, Plug } from "lucide-react";
import type { ReactNode } from "react";
import { stringify } from "yaml";
import { EntityActions } from "@/components/actions/entity-actions";
import { type RunsSearch, RunsTable, validateRunsSearch } from "@/components/actions/runs-table";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { EntityLink } from "@/components/catalog/entity-link";
import { ListValue } from "@/components/catalog/list-value";
import { PropertyLabel } from "@/components/catalog/property-label";
import {
  CatalogViewAll,
  EmptyValue,
  isWebUrl,
  PropertyValue,
} from "@/components/catalog/property-value";
import { SplitTitle } from "@/components/catalog/split-title";
import { UnpublishedBlueprint } from "@/components/catalog/unpublished-blueprint";
import { Detail, DetailList } from "@/components/detail-list";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageError } from "@/components/page-error";
import { PageHeader } from "@/components/page-header";
import { PageTab } from "@/components/page-tabs";
import { ScorecardResult, ScorecardTab } from "@/components/scorecards/entity-scorecards";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList } from "@/components/ui/tabs";
import { WidgetGrid } from "@/components/widgets/widget-grid";
import { availableQueryOptions, runsQueryOptions } from "@/features/actions";
import { hasRole, useMe } from "@/features/auth";
import { type BlueprintSpec, blueprints } from "@/features/blueprints";
import {
  type Link as CatalogLink,
  type EntityDetail,
  entityQueryOptions,
  type Incoming,
} from "@/features/catalog";
import { entityScorecardsQueryOptions } from "@/features/scorecards";
import { useUiState } from "@/hooks/use-ui-state";
import { formatAbsolute, formatRelative } from "@/lib/format";

// The blueprint's entity tabs and each scorecard that scores the entity get
// a tab, between Overview and the system tabs. A blueprint tab named
// overview takes the built-in Overview's place.
const systemTabs = ["runs", "relations", "yaml"] as const;

// The runs tab keeps its table's state beside the open tab.
function validateEntitySearch(search: Record<string, unknown>): RunsSearch & { tab?: string } {
  const tab = typeof search.tab === "string" && search.tab !== "" ? search.tab : undefined;
  return { ...(tab ? { tab } : {}), ...(tab === "runs" ? validateRunsSearch(search) : {}) };
}

export const Route = createFileRoute("/_app/catalog/$blueprint/$")({
  validateSearch: validateEntitySearch,
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

const noTabs: Record<string, string> = {};

function EntityPage() {
  const { blueprint, _splat: identifier = "" } = Route.useParams();
  // Without a tab in the URL, the page opens the one last opened for its
  // blueprint.
  const [lastTabs, setLastTabs] = useUiState("entityTabs", noTabs);
  const { tab = lastTabs[blueprint] ?? "overview", ...runsSearch } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const builder = hasRole(useMe().role, "builder");
  const { data: doc } = useSuspenseQuery(blueprints.queries.detail(blueprint));
  const { data: entity } = useSuspenseQuery(entityQueryOptions(blueprint, identifier));
  const { data: results } = useSuspenseQuery(entityScorecardsQueryOptions(blueprint, identifier));
  // Runs get a tab once there's something to run or something ran.
  const { data: available = [] } = useQuery(availableQueryOptions(blueprint, identifier));
  const runs = useQuery(runsQueryOptions({ blueprint, entity: identifier })).data?.total ?? 0;
  const spec = doc.published?.spec;
  if (!spec) {
    return <UnpublishedBlueprint blueprint={blueprint} title={doc.title} page={entity.title} />;
  }
  const header = spec.entity?.header;
  const subtitle = header?.subtitle ? propertyByID(spec, header.subtitle) : undefined;
  const badges = (header?.badges ?? []).flatMap((id) => propertyByID(spec, id) ?? []);
  const linkValue = header?.link ? entity.properties[header.link] : undefined;
  const link = typeof linkValue === "string" && isWebUrl(linkValue) ? linkValue : undefined;
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

  return (
    <Tabs
      value={active}
      onValueChange={(next) => {
        setLastTabs({ ...lastTabs, [blueprint]: next });
        void navigate({ search: { tab: next }, replace: true });
      }}
    >
      <PageHeader
        title={entity.title}
        heading={<SplitTitle title={entity.title} />}
        breadcrumbs={
          <PageBreadcrumbs page={entity.title}>
            <Link to="/catalog">Catalog</Link>
            <Link to="/catalog/$blueprint" params={{ blueprint }}>
              {spec.title}
            </Link>
          </PageBreadcrumbs>
        }
        badges={badges.map((property) => (
          <PropertyValue
            key={property.identifier}
            property={property}
            value={entity.properties[property.identifier]}
            links={entity.related[property.identifier]}
            entity={entity}
            compact
          />
        ))}
        description={
          <div className="grid gap-1">
            {subtitle && (
              <div>
                <PropertyValue
                  property={subtitle}
                  value={entity.properties[subtitle.identifier]}
                  links={entity.related[subtitle.identifier]}
                  entity={entity}
                />
              </div>
            )}
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <code className="font-mono">{entity.identifier}</code>
              <span className="flex items-center gap-1">
                <Plug className="size-3.5" aria-hidden />
                {/* Integration pages are for builders, so members see only its name. */}
                {builder ? (
                  <Link
                    to="/integrations/$identifier"
                    params={{ identifier: entity.integration }}
                    className="hover:underline"
                  >
                    {entity.integration}
                  </Link>
                ) : (
                  entity.integration
                )}
              </span>
              <span title={formatAbsolute(entity.updatedAt)}>
                Updated {formatRelative(entity.updatedAt)}
              </span>
            </p>
          </div>
        }
        actions={
          <>
            <EntityActions
              entity={{ blueprint, identifier: entity.identifier, title: entity.title }}
            />
            {link && (
              <Button variant="outline" asChild>
                <a href={link} target="_blank" rel="noreferrer" title={link}>
                  <ExternalLink />
                  Open
                </a>
              </Button>
            )}
          </>
        }
        tabs={
          <TabsList variant="line" aria-label="Entity">
            <PageTab value="overview">{overview?.title ?? "Overview"}</PageTab>
            {views.map((v) => (
              <PageTab key={v.identifier} value={v.identifier}>
                {v.title}
              </PageTab>
            ))}
            {results.map((result) => (
              <PageTab key={result.scorecard} value={result.scorecard}>
                <ScorecardTab result={result} />
              </PageTab>
            ))}
            {(available.length > 0 || runs > 0) && (
              <PageTab value="runs">
                Runs
                <span className="text-xs text-muted-foreground tabular-nums">{runs}</span>
              </PageTab>
            )}
            <PageTab value="relations">
              Relations
              <span className="text-xs text-muted-foreground tabular-nums">
                {relationCount(spec, entity)}
              </span>
            </PageTab>
            <PageTab value="yaml">YAML</PageTab>
          </TabsList>
        }
      />
      <TabsContent value="overview" className="pt-2">
        {overview ? (
          <WidgetGrid
            widgets={overview.widgets}
            source={{ blueprint, page: "entity", tab: overview.identifier }}
            context={{ entity: identifier }}
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
            context={{ entity: identifier }}
          />
        </TabsContent>
      ))}
      {results.map((result) => (
        <TabsContent key={result.scorecard} value={result.scorecard} className="pt-2">
          <ScorecardResult result={result} />
        </TabsContent>
      ))}
      <TabsContent value="runs" className="pt-2">
        <RunsTable
          scope={{ blueprint, entity: identifier }}
          search={runsSearch}
          hide={["entity"]}
          empty={{
            title: "Nothing has run here yet",
            description: "Runs of actions on this entity show up here.",
          }}
        />
      </TabsContent>
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
  );
}

function propertyByID(spec: BlueprintSpec, id: string) {
  return spec.properties.find((property) => property.identifier === id);
}

// overviewSections groups the shown properties as the blueprint's sections
// say, leaving out those in the header; the rest follow in their own card.
function overviewSections(spec: BlueprintSpec) {
  const header = spec.entity?.header;
  const inHeader = new Set([header?.subtitle, header?.link, ...(header?.badges ?? [])]);
  const shown = spec.properties.filter(
    (property) => !property.display?.hidden && !inHeader.has(property.identifier),
  );
  const sections = (spec.entity?.sections ?? [])
    .map((section) => ({
      title: section.title,
      properties: section.properties.flatMap(
        (id) => shown.find((property) => property.identifier === id) ?? [],
      ),
    }))
    .filter((section) => section.properties.length > 0);
  const placed = new Set(
    sections.flatMap((section) => section.properties.map((p) => p.identifier)),
  );
  const rest = shown.filter((property) => !placed.has(property.identifier));
  if (rest.length > 0) {
    sections.push({ title: sections.length > 0 ? "Other" : "Properties", properties: rest });
  }

  return sections;
}

function relationCount(spec: BlueprintSpec, entity: EntityDetail) {
  const outgoing = spec.properties.reduce(
    (sum, property) => sum + (entity.related[property.identifier]?.length ?? 0),
    0,
  );
  return outgoing + entity.incoming.reduce((sum, incoming) => sum + incoming.total, 0);
}

function Overview({ spec, entity }: Readonly<{ spec: BlueprintSpec; entity: EntityDetail }>) {
  const sections = overviewSections(spec);
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <div className="grid gap-4">
        {sections.length === 0 ? (
          <Card>
            <CardContent>
              <p className="text-sm text-muted-foreground">This blueprint has no properties.</p>
            </CardContent>
          </Card>
        ) : (
          sections.map((section) => (
            <Card key={section.title}>
              <CardTitleBar title={section.title} />
              <CardContent>
                <DetailList>
                  {section.properties.map((property) => (
                    <Detail key={property.identifier} label={<PropertyLabel property={property} />}>
                      <PropertyValue
                        property={property}
                        value={entity.properties[property.identifier]}
                        links={entity.related[property.identifier]}
                        entity={entity}
                      />
                    </Detail>
                  ))}
                </DetailList>
              </CardContent>
            </Card>
          ))
        )}
      </div>
      <RelationsSummary spec={spec} entity={entity} />
    </div>
  );
}

// IncomingLink opens the other blueprint's catalog, filtered to the entities
// that point at this one.
function IncomingLink({
  incoming,
  identifier,
  children,
}: Readonly<{ incoming: Incoming; identifier: string; children: ReactNode }>) {
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

// RelationRow is one relation of an entity's: a relation property of its
// own, or one of another blueprint's that points at it.
type RelationRow = {
  key: string;
  title: string;
  incoming: boolean;
  links: CatalogLink[];
  total: number;
  // viewAll opens every related entity in the catalog.
  viewAll: ReactNode;
};

type RelationGroup = { blueprint: string; rows: RelationRow[] };

// relationGroups gathers an entity's relations by the blueprint they reach,
// in the order its properties name them, then those pointing at it.
function relationGroups(spec: BlueprintSpec, entity: EntityDetail): RelationGroup[] {
  const groups: RelationGroup[] = [];
  const group = (blueprint: string) => {
    let found = groups.find((g) => g.blueprint === blueprint);
    if (!found) {
      found = { blueprint, rows: [] };
      groups.push(found);
    }
    return found;
  };
  for (const property of spec.properties) {
    if (!property.relation || property.lookup) continue;
    const links = entity.related[property.identifier] ?? [];
    group(property.relation).rows.push({
      key: property.identifier,
      title: property.title,
      incoming: false,
      links,
      total: links.length,
      viewAll: (
        <CatalogViewAll
          blueprint={property.relation}
          identifiers={links.map((link) => link.identifier)}
        />
      ),
    });
  }
  for (const incoming of entity.incoming) {
    group(incoming.blueprint).rows.push({
      key: `${incoming.blueprint}.${incoming.property}`,
      title: incoming.propertyTitle,
      incoming: true,
      links: incoming.entities,
      total: incoming.total,
      viewAll: (
        <IncomingLink incoming={incoming} identifier={entity.identifier}>
          View all
        </IncomingLink>
      ),
    });
  }
  return groups;
}

// RowTitle names a relation, with ← for one that points here from the
// group's blueprint.
function RowTitle({ row }: Readonly<{ row: RelationRow }>) {
  return (
    <span className="flex items-center gap-1 text-muted-foreground">
      {row.incoming && <ArrowLeft className="size-3.5" aria-label="Pointing here by" />}
      {row.title}
    </span>
  );
}

function Relations({ spec, entity }: Readonly<{ spec: BlueprintSpec; entity: EntityDetail }>) {
  const groups = relationGroups(spec, entity);
  if (groups.length === 0) {
    return (
      <Card>
        <CardContent>
          <p className="text-sm text-muted-foreground">Nothing relates to this entity.</p>
        </CardContent>
      </Card>
    );
  }
  return (
    <Card className="gap-0 py-0">
      {groups.map((g) => (
        <CardSection key={g.blueprint} heading={<BlueprintLabel blueprint={g.blueprint} />}>
          <DetailList>
            {g.rows.map((row) => (
              <Detail key={row.key} label={<RowTitle row={row} />}>
                {row.total === 0 ? (
                  <EmptyValue />
                ) : (
                  <RelationLinks
                    blueprint={g.blueprint}
                    links={row.links}
                    total={row.total}
                    viewAll={row.viewAll}
                  />
                )}
              </Detail>
            ))}
          </DetailList>
        </CardSection>
      ))}
    </Card>
  );
}

// RelationsSummary counts an entity's relations by blueprint, each count
// opening the Relations tab.
function RelationsSummary({
  spec,
  entity,
}: Readonly<{ spec: BlueprintSpec; entity: EntityDetail }>) {
  const groups = relationGroups(spec, entity);
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Relations" />
      <CardContent className="grid px-0 text-sm">
        {groups.length === 0 ? (
          <p className="px-4 py-3 text-muted-foreground">Nothing relates to this entity.</p>
        ) : (
          groups.map((g) => (
            <CardSection key={g.blueprint} heading={<BlueprintLabel blueprint={g.blueprint} />}>
              <ul className="grid gap-1">
                {g.rows.map((row) => (
                  <li key={row.key} className="flex items-center justify-between gap-3">
                    <RowTitle row={row} />
                    <Link
                      to="."
                      search={{ tab: "relations" }}
                      className="font-medium tabular-nums hover:underline"
                    >
                      {row.total}
                    </Link>
                  </li>
                ))}
              </ul>
            </CardSection>
          ))
        )}
      </CardContent>
    </Card>
  );
}

function RelationLinks({
  blueprint,
  links,
  total,
  viewAll,
}: Readonly<{ blueprint: string; links: CatalogLink[]; total?: number; viewAll: ReactNode }>) {
  return (
    <ListValue
      total={total}
      viewAll={viewAll}
      items={links.map((link) => ({
        key: link.identifier,
        node: link.title ? (
          <EntityLink blueprint={blueprint} identifier={link.identifier}>
            <SplitTitle title={link.title} />
          </EntityLink>
        ) : (
          <span className="font-mono text-xs text-muted-foreground" title="Not ingested yet">
            {link.identifier}
          </span>
        ),
      }))}
    />
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
