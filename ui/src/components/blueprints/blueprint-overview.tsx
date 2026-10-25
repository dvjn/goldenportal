import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  ArrowLeft,
  ArrowRight,
  Calculator,
  Database,
  Link2,
  Lock,
  type LucideIcon,
  Plug,
  Repeat2,
} from "lucide-react";
import type { ReactNode } from "react";
import { PropertyType } from "@/components/blueprints/property-type";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import {
  type Blueprint,
  type BlueprintSpec,
  type Property,
  propertySource,
  type Usage,
  usageQueryOptions,
} from "@/features/blueprints";
import { formatAbsolute, formatRelative } from "@/lib/format";

const sources: Record<ReturnType<typeof propertySource>, { label: string; icon: LucideIcon }> = {
  stored: { label: "Stored", icon: Database },
  relation: { label: "Relation", icon: Link2 },
  lookup: { label: "Lookup", icon: Repeat2 },
  compute: { label: "Computed", icon: Calculator },
};

// BlueprintOverview shows a blueprint read-only: what each property holds,
// the rules values must follow, where they come from, and how the blueprint
// relates to others.
export function BlueprintOverview({
  spec,
  blueprint,
}: Readonly<{ spec: BlueprintSpec; blueprint: Blueprint }>) {
  const usage = useQuery(usageQueryOptions(spec.identifier)).data;

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="gap-0 pb-0">
        <CardHeader className="border-b">
          <CardTitle>Properties</CardTitle>
        </CardHeader>
        <CardContent className="px-0">
          <PropertiesTable properties={spec.properties} />
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <AboutCard spec={spec} blueprint={blueprint} usage={usage} />
        <WritersCard spec={spec} usage={usage} />
        <RelationsCard spec={spec} usage={usage} />
        <PagesCard spec={spec} published={blueprint.published !== null} />
      </div>
    </div>
  );
}

function PropertiesTable({ properties }: Readonly<{ properties: Property[] }>) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-4">Property</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Rules</TableHead>
          <TableHead className="pr-4">Source</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {builtIns.map((builtIn) => (
          <TableRow key={builtIn.identifier} className="align-top text-muted-foreground">
            <TableCell className="pl-4 whitespace-normal">
              <div className="grid gap-0.5">
                <span className="font-medium">{builtIn.title}</span>
                <code className="font-mono text-xs">{builtIn.identifier}</code>
              </div>
            </TableCell>
            <TableCell>
              <Badge variant="secondary" className="font-mono">
                string
              </Badge>
            </TableCell>
            <TableCell className="whitespace-normal">
              {builtIn.identifier === "$identifier"
                ? "Unique, required"
                : "Defaults to the identifier"}
            </TableCell>
            <TableCell className="pr-4">
              <span className="flex items-center gap-1.5 text-xs">
                <Lock className="size-3.5" aria-hidden />
                Built-in
              </span>
            </TableCell>
          </TableRow>
        ))}
        {properties.map((property) => (
          <TableRow key={property.identifier} className="align-top">
            <TableCell className="pl-4 whitespace-normal">
              <div className="grid gap-0.5">
                <span className="font-medium">{property.title}</span>
                <code className="font-mono text-xs text-muted-foreground">
                  {property.identifier}
                </code>
                {property.description && (
                  <span className="text-xs text-muted-foreground">{property.description}</span>
                )}
              </div>
            </TableCell>
            <TableCell>
              <PropertyType property={property} />
            </TableCell>
            <TableCell className="whitespace-normal">
              <Rules property={property} />
            </TableCell>
            <TableCell className="pr-4 whitespace-normal">
              <Source property={property} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

// Every entity has these, whatever its blueprint says.
const builtIns = [
  { identifier: "$identifier", title: "Identifier" },
  { identifier: "$title", title: "Title" },
];

function Rules({ property }: Readonly<{ property: Property }>) {
  const rules: ReactNode[] = [];
  if (property.required) {
    rules.push(
      <Badge key="required" variant="outline" className="border-primary/40 text-primary">
        Required
      </Badge>,
    );
  }
  const values = property.enum ?? property.items?.enum;
  for (const value of values ?? []) {
    rules.push(
      <Badge key={`enum:${value}`} variant="outline" className="font-mono">
        {String(value)}
      </Badge>,
    );
  }
  const range = (min?: number, max?: number, unit = "") => {
    if (min !== undefined && max !== undefined) return `${min}–${max}${unit}`;
    if (min !== undefined) return `≥ ${min}${unit}`;
    if (max !== undefined) return `≤ ${max}${unit}`;
  };
  const length = range(property.minLength, property.maxLength, " chars");
  if (length) rules.push(<RuleText key="length">{length}</RuleText>);
  const bounds = range(property.minimum, property.maximum);
  if (bounds) rules.push(<RuleText key="bounds">{bounds}</RuleText>);
  if (property.pattern) {
    rules.push(
      <code key="pattern" className="rounded bg-muted px-1 font-mono text-xs">
        /{property.pattern}/
      </code>,
    );
  }

  if (rules.length === 0) return <span className="text-muted-foreground">—</span>;
  return <div className="flex flex-wrap items-center gap-1">{rules}</div>;
}

function RuleText({ children }: Readonly<{ children: ReactNode }>) {
  return <span className="text-xs text-muted-foreground">{children}</span>;
}

function Source({ property }: Readonly<{ property: Property }>) {
  const source = sources[propertySource(property)];
  const detail = property.lookup ?? property.compute;

  return (
    <div className="grid gap-1">
      <span className="flex items-center gap-1.5 text-xs">
        <source.icon className="size-3.5 text-muted-foreground" aria-hidden />
        {source.label}
      </span>
      {detail && (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <code className="block max-w-56 truncate rounded bg-muted px-1 font-mono text-xs">
                {detail}
              </code>
            </TooltipTrigger>
            <TooltipContent className="max-w-sm font-mono">{detail}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
    </div>
  );
}

function AboutCard({
  spec,
  blueprint,
  usage,
}: Readonly<{ spec: BlueprintSpec; blueprint: Blueprint; usage?: Usage }>) {
  const published = blueprint.published;

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>About</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <dl className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-2">
          <dt className="text-muted-foreground">Entities</dt>
          <dd className="font-medium tabular-nums">{usage?.entities ?? "–"}</dd>
          {published && (
            <>
              <dt className="text-muted-foreground">Published</dt>
              <dd>
                Version {published.version},{" "}
                <span title={formatAbsolute(published.publishedAt)}>
                  {formatRelative(published.publishedAt)}
                </span>
              </dd>
              {published.publishedBy && (
                <>
                  <dt className="text-muted-foreground">By</dt>
                  <dd className="truncate">{published.publishedBy}</dd>
                </>
              )}
            </>
          )}
        </dl>
      </CardContent>
    </Card>
  );
}

// WritersCard lists the integrations that write the blueprint's entities.
// Each entity has exactly one writer, so this is where its data comes from.
function WritersCard({ spec, usage }: Readonly<{ spec: BlueprintSpec; usage?: Usage }>) {
  const writers = usage?.writers ?? [];

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>Written By</CardTitle>
      </CardHeader>
      <CardContent>
        {writers.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No integration writes {spec.identifier} entities yet.{" "}
            <Link to="/integrations/new" className="text-foreground underline underline-offset-4">
              New Integration
            </Link>
          </p>
        ) : (
          <ul className="grid gap-3">
            {writers.map((writer) => (
              <li key={writer.integration} className="flex items-start gap-2 text-sm">
                <Plug className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
                <div className="grid min-w-0 flex-1">
                  <Link
                    to="/integrations/$identifier"
                    params={{ identifier: writer.integration }}
                    className="truncate font-medium hover:underline"
                  >
                    {writer.title}
                  </Link>
                  <span className="text-xs text-muted-foreground">
                    {writer.entities} {writer.entities === 1 ? "entity" : "entities"}
                    {writer.lastRunAt ? (
                      <span title={formatAbsolute(writer.lastRunAt)}>
                        {" "}
                        · pushed {formatRelative(writer.lastRunAt)}
                      </span>
                    ) : (
                      " · never pushed"
                    )}
                  </span>
                </div>
                {!writer.mapped && (
                  <Badge variant="outline" title="Its mappings no longer write this blueprint">
                    Unmapped
                  </Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

// PagesCard lists the tabs of the blueprint's catalog page and its
// entities' pages, its own beside those every page has.
function PagesCard({ spec, published }: Readonly<{ spec: BlueprintSpec; published: boolean }>) {
  const catalog = spec.catalog?.tabs ?? [];
  const entity = spec.entity?.tabs ?? [];
  const tab = (title: string, widgets?: number) => (
    <>
      {title}
      {widgets !== undefined && (
        <span className="text-xs text-muted-foreground">
          {" "}
          · {widgets} {widgets === 1 ? "widget" : "widgets"}
        </span>
      )}
    </>
  );

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>Pages</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 text-sm">
        <section className="grid gap-1.5">
          <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            Catalog Page
          </h3>
          <ul className="grid gap-1">
            {catalog.map((t) => (
              <li key={t.identifier}>
                {published ? (
                  <Link
                    to="/catalog/$blueprint"
                    params={{ blueprint: spec.identifier }}
                    search={{ view: t.identifier }}
                    className="hover:underline"
                  >
                    {tab(t.title, t.widgets.length)}
                  </Link>
                ) : (
                  tab(t.title, t.widgets.length)
                )}
              </li>
            ))}
            <li className="text-muted-foreground">Entities</li>
          </ul>
        </section>
        <section className="grid gap-1.5">
          <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            Entity Pages
          </h3>
          <ul className="grid gap-1">
            {!entity.some((t) => t.identifier === "overview") && (
              <li className="text-muted-foreground">Overview</li>
            )}
            {entity.map((t) => (
              <li key={t.identifier}>{tab(t.title, t.widgets.length)}</li>
            ))}
            <li className="text-muted-foreground">Scorecards, Relations, YAML</li>
          </ul>
        </section>
      </CardContent>
    </Card>
  );
}

function RelationsCard({ spec, usage }: Readonly<{ spec: BlueprintSpec; usage?: Usage }>) {
  const outgoing = spec.properties.filter((property) => property.relation);
  const incoming = usage?.references ?? [];

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>Relations</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        <RelationList title="Points To" empty="No relations to other blueprints.">
          {outgoing.map((property) => (
            <RelationRow
              key={property.identifier}
              icon={ArrowRight}
              blueprint={property.relation ?? ""}
              via={property.title}
              many={property.many}
            />
          ))}
        </RelationList>
        <RelationList title="Pointed At By" empty="No blueprint relates to this one.">
          {incoming.map((reference) => (
            <RelationRow
              key={`${reference.blueprint}.${reference.property}`}
              icon={ArrowLeft}
              blueprint={reference.blueprint}
              via={reference.propertyTitle}
              many={reference.many}
            />
          ))}
        </RelationList>
      </CardContent>
    </Card>
  );
}

function RelationList({
  title,
  empty,
  children,
}: Readonly<{ title: string; empty: string; children: ReactNode[] }>) {
  return (
    <section className="grid gap-2">
      <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
      {children.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <ul className="grid gap-2">{children}</ul>
      )}
    </section>
  );
}

function RelationRow({
  icon: Icon,
  blueprint,
  via,
  many,
}: Readonly<{ icon: LucideIcon; blueprint: string; via: string; many?: boolean }>) {
  return (
    <li className="flex items-center gap-2 text-sm">
      <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <Link
        to="/blueprints/$identifier"
        params={{ identifier: blueprint }}
        className="font-mono hover:underline"
      >
        {blueprint}
      </Link>
      <span className="truncate text-muted-foreground">
        via {via}
        {many && " (many)"}
      </span>
    </li>
  );
}
