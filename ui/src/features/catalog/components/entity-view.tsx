import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLinkIcon } from "lucide-react";
import { useBreadcrumbAncestors } from "@/components/common/breadcrumb-trail";
import { DescriptionList, DescriptionRow } from "@/components/common/description-list";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { StateCard } from "@/components/common/state-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { formatRelative } from "@/lib/time";
import { usePageTitle } from "@/lib/use-page-title";
import { cn } from "@/lib/utils";
import {
  type Blueprint,
  type DerivedView,
  type Entity,
  isNotFound,
  type PropertyView,
} from "../api";
import { derivedByKey, propertyByKey, visibleProperties } from "../columns";
import { hostLabel, isSafeUrl } from "../format";
import { blueprintQueryOptions, entityQueryOptions, useSourceIcons } from "../queries";
import { BlueprintIcon } from "./blueprint-icon";
import { DerivedValue } from "./derived-value";
import { DescribedLabel } from "./described-label";
import { EntityActions } from "./entity-actions";
import { EntityRelationsPanel } from "./entity-relations-panel";
import { EnumBadge, enumColor, enumLabel } from "./enum-badge";
import { PropertyValue, Timestamp } from "./property-value";
import { SourceMark } from "./source-mark";
import { SplitTitle } from "./split-title";

export type SectionField =
  | { kind: "property"; key: string; property: PropertyView }
  | { kind: "derived"; key: string; derived: DerivedView };

export type PropertySection = { title: string; fields: SectionField[] };

function propertyField(property: PropertyView): SectionField {
  return { kind: "property", key: property.key, property };
}

function derivedField(derived: DerivedView): SectionField {
  return { kind: "derived", key: derived.key, derived };
}

function groupField(blueprint: Blueprint, key: string): SectionField | undefined {
  const property = propertyByKey(blueprint, key);
  if (property) return property.hidden ? undefined : propertyField(property);
  const derived = derivedByKey(blueprint, key);
  return derived ? derivedField(derived) : undefined;
}

export function propertySections(
  blueprint: Blueprint,
  shownInHeader: readonly string[] = [],
): PropertySection[] {
  const groups = (blueprint.display.groups ?? []).map((group) => ({
    title: group.title,
    fields: group.properties
      .filter((key) => !shownInHeader.includes(key))
      .map((key) => groupField(blueprint, key))
      .filter((field): field is SectionField => field !== undefined),
  }));
  const placed = new Set([
    ...shownInHeader,
    ...groups.flatMap((group) => group.fields.map(({ key }) => key)),
  ]);
  const details = {
    title: "Details",
    fields: [
      ...visibleProperties(blueprint).map(propertyField),
      ...blueprint.derived.map(derivedField),
    ].filter(({ key }) => !placed.has(key)),
  };
  return [...groups, details].filter((section) => section.fields.length > 0);
}

function visibleEntries(entity: Entity, blueprint: Blueprint) {
  return visibleProperties(blueprint).map((property) => ({
    property,
    value: entity.properties[property.key],
  }));
}

function badgeEntries(entity: Entity, blueprint: Blueprint) {
  const keys = blueprint.display.badges ?? [];
  return visibleEntries(entity, blueprint).filter(
    ({ property, value }) =>
      keys.includes(property.key) &&
      ((property.enum && typeof value === "string" && value !== "") ||
        (property.type === "boolean" && value === true)),
  );
}

function headerLink(entity: Entity, blueprint: Blueprint) {
  return visibleEntries(entity, blueprint).find(
    ({ property, value }) =>
      property.format === "url" && typeof value === "string" && isSafeUrl(value),
  );
}

function subtitleEntry(entity: Entity, blueprint: Blueprint) {
  const property = propertyByKey(blueprint, blueprint.display.subtitle);
  const value = property && entity.properties[property.key];
  if (!property || typeof value !== "string" || value === "") return undefined;
  return { property, value };
}

function headerFields(entity: Entity, blueprint: Blueprint) {
  const shown = [headerLink(entity, blueprint), subtitleEntry(entity, blueprint)]
    .filter((entry) => entry !== undefined)
    .map(({ property }) => property.key);
  return [...shown, ...(blueprint.display.badges ?? [])];
}

function MetaBadges({ entity, blueprint }: Readonly<{ entity: Entity; blueprint: Blueprint }>) {
  return badgeEntries(entity, blueprint).map(({ property, value }) => {
    const title = property.title || property.key;
    if (typeof value === "string") {
      return (
        <EnumBadge
          key={property.key}
          label={`${title}: ${enumLabel(property, value)}`}
          color={enumColor(property, value)}
        />
      );
    }
    return (
      <Badge key={property.key} variant="muted">
        {title}
      </Badge>
    );
  });
}

function OpenExternal({ entity, blueprint }: Readonly<{ entity: Entity; blueprint: Blueprint }>) {
  const link = headerLink(entity, blueprint);
  if (!link) return null;
  const url = String(link.value);

  return (
    <Button asChild>
      <a href={url} target="_blank" rel="noreferrer">
        {link.property.link_label || `Open on ${hostLabel(url)}`}
        <ExternalLinkIcon aria-hidden="true" />
      </a>
    </Button>
  );
}

function SectionFieldRow({ field, entity }: Readonly<{ field: SectionField; entity: Entity }>) {
  if (field.kind === "derived") {
    const { derived } = field;
    return (
      <DescriptionRow
        label={
          <DescribedLabel
            label={derived.title || derived.key}
            description={derived.description ?? derived.property?.description}
          />
        }
      >
        <DerivedValue derived={derived} items={entity.derived[derived.key]} />
      </DescriptionRow>
    );
  }
  const { property } = field;
  return (
    <DescriptionRow
      label={
        <DescribedLabel label={property.title || property.key} description={property.description} />
      }
    >
      <PropertyValue value={entity.properties[property.key]} property={property} />
    </DescriptionRow>
  );
}

function PropertySectionCard({
  section,
  entity,
}: Readonly<{ section: PropertySection; entity: Entity }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{section.title}</CardTitle>
      </CardHeader>
      <CardContent>
        <DescriptionList className="gap-x-8 gap-y-4 sm:grid-cols-2">
          {section.fields.map((field) => (
            <SectionFieldRow key={field.key} field={field} entity={entity} />
          ))}
        </DescriptionList>
      </CardContent>
    </Card>
  );
}

function RecordCard({ entity, sourceIcon }: Readonly<{ entity: Entity; sourceIcon: string }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Record</CardTitle>
      </CardHeader>
      <CardContent>
        <DescriptionList className="gap-x-8 gap-y-4 sm:grid-cols-2">
          <DescriptionRow label="Identifier">
            <span className="font-mono text-xs break-all">{entity.identifier}</span>
          </DescriptionRow>
          <DescriptionRow label="Integration">
            <Link
              to="/settings/catalog-setup"
              search={{ tab: "integrations", integration: entity.integration.identifier }}
              className="inline-flex items-center gap-1.5 text-primary underline-offset-4 hover:underline"
            >
              <SourceMark icon={sourceIcon} className="size-3.5" />
              {entity.integration.title}
            </Link>
          </DescriptionRow>
          <DescriptionRow label="Created">
            <Timestamp value={entity.created_at} />
          </DescriptionRow>
          <DescriptionRow label="Updated">
            <Timestamp value={entity.updated_at} />
          </DescriptionRow>
        </DescriptionList>
      </CardContent>
    </Card>
  );
}

function EntityOverview({
  entity,
  blueprint,
  sourceIcon,
}: Readonly<{ entity: Entity; blueprint: Blueprint; sourceIcon: string }>) {
  const sections = propertySections(blueprint, headerFields(entity, blueprint));

  return (
    <div className="flex flex-col gap-4">
      {sections.map((section) => (
        <PropertySectionCard key={section.title} section={section} entity={entity} />
      ))}
      <EntityRelationsPanel entity={entity} />
      <RecordCard entity={entity} sourceIcon={sourceIcon} />
    </div>
  );
}

function EntityBody({ entity, blueprint }: Readonly<{ entity: Entity; blueprint: Blueprint }>) {
  const entityTitle = entity.title || entity.identifier;
  const sourceIcon = useSourceIcons().data?.ofIntegration(entity.integration.identifier) ?? "";
  const subtitleProperty = subtitleEntry(entity, blueprint);
  const subtitle = subtitleProperty?.value ?? entity.identifier;
  const monoSubtitle = !subtitleProperty || subtitleProperty.property.display === "mono";
  useBreadcrumbAncestors([
    { label: blueprint.title, to: `/catalog/${encodeURIComponent(blueprint.identifier)}` },
  ]);
  usePageTitle(entityTitle);

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight break-all">
            <SplitTitle title={entityTitle} />
          </h1>
          {subtitle !== entityTitle ? (
            <p
              className={cn("text-sm break-all text-muted-foreground", monoSubtitle && "font-mono")}
            >
              {subtitle}
            </p>
          ) : null}
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <Badge asChild variant="outline">
              <Link to="/catalog/$blueprint" params={{ blueprint: blueprint.identifier }}>
                <BlueprintIcon icon={blueprint.icon} />
                {blueprint.title}
              </Link>
            </Badge>
            <SourceMark icon={sourceIcon} className="size-4" />
            <MetaBadges entity={entity} blueprint={blueprint} />
            <span>Updated {formatRelative(entity.updated_at)}</span>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EntityActions entity={entity} />
          <OpenExternal entity={entity} blueprint={blueprint} />
        </div>
      </div>
      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="json">JSON</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <EntityOverview entity={entity} blueprint={blueprint} sourceIcon={sourceIcon} />
        </TabsContent>
        <TabsContent value="json">
          <Card>
            <CardContent>
              <pre className="max-h-[32rem] overflow-auto rounded-lg bg-muted p-4 font-mono text-xs">
                {JSON.stringify(entity, null, 2)}
              </pre>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </>
  );
}

export function EntityView({
  blueprintId,
  identifier,
}: Readonly<{ blueprintId: string; identifier: string }>) {
  const entity = useQuery({
    ...entityQueryOptions(blueprintId, identifier),
    retry: (failureCount, error) => !isNotFound(error) && failureCount < 1,
  });
  const blueprint = useQuery(blueprintQueryOptions(blueprintId));

  if (isNotFound(entity.error) || isNotFound(blueprint.error)) {
    return (
      <StateCard title="Entity not found" message={`No entity has the identifier "${identifier}".`}>
        <Button asChild variant="outline" size="sm">
          <Link to="/catalog/$blueprint" params={{ blueprint: blueprintId }}>
            Back to the list
          </Link>
        </Button>
      </StateCard>
    );
  }
  if (entity.isError || blueprint.isError) {
    return <StateCard title="Entity unavailable" message="We could not load this entity." />;
  }
  if (!entity.data || !blueprint.data) return <ListSkeleton />;

  return <EntityBody entity={entity.data} blueprint={blueprint.data} />;
}
