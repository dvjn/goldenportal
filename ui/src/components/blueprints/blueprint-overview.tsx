import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  ArrowLeft,
  ArrowRight,
  Calculator,
  Database,
  Link2,
  type LucideIcon,
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
  type BlueprintSpec,
  type Property,
  propertySource,
  referencesQueryOptions,
} from "@/features/blueprints";
import { entitiesQueryOptions } from "@/features/entities";

const sources: Record<ReturnType<typeof propertySource>, { label: string; icon: LucideIcon }> = {
  stored: { label: "Stored", icon: Database },
  relation: { label: "Relation", icon: Link2 },
  lookup: { label: "Lookup", icon: Repeat2 },
  compute: { label: "Computed", icon: Calculator },
};

// BlueprintOverview shows a blueprint read-only: what each property holds,
// the rules values must follow, where they come from, and how the blueprint
// relates to others.
export function BlueprintOverview({ spec }: Readonly<{ spec: BlueprintSpec }>) {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="gap-0 pb-0">
        <CardHeader className="border-b">
          <CardTitle>Properties</CardTitle>
        </CardHeader>
        <CardContent className="px-0">
          {spec.properties.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">No properties yet.</p>
          ) : (
            <PropertiesTable properties={spec.properties} />
          )}
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <AboutCard spec={spec} />
        <RelationsCard spec={spec} />
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

function AboutCard({ spec }: Readonly<{ spec: BlueprintSpec }>) {
  const entities = useQuery(entitiesQueryOptions({ blueprint: spec.identifier, size: 10 }));
  const count = (source: ReturnType<typeof propertySource>) =>
    spec.properties.filter((property) => propertySource(property) === source).length;
  const stats = [
    { label: "Entities", value: entities.data?.total ?? "–" },
    { label: "Stored", value: count("stored") },
    { label: "Relations", value: count("relation") },
    { label: "Derived", value: count("lookup") + count("compute") },
  ];

  return (
    <Card>
      <CardHeader className="border-b">
        <CardTitle>About</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        {spec.description && <p className="text-sm text-muted-foreground">{spec.description}</p>}
        <dl className="grid grid-cols-2 gap-3">
          {stats.map((stat) => (
            <div key={stat.label} className="grid gap-0.5 rounded-md border p-2">
              <dt className="text-xs text-muted-foreground">{stat.label}</dt>
              <dd className="text-lg font-semibold tabular-nums">{stat.value}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

function RelationsCard({ spec }: Readonly<{ spec: BlueprintSpec }>) {
  const references = useQuery(referencesQueryOptions(spec.identifier));
  const outgoing = spec.properties.filter((property) => property.relation);
  const incoming = references.data ?? [];

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
