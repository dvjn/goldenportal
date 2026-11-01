import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { cn } from "cn";
import {
  Calculator,
  Database,
  Link2,
  Lock,
  type LucideIcon,
  Repeat2,
  ShieldCheck,
} from "lucide-react";
import type { ReactNode } from "react";
import { PropertyType } from "@/components/blueprints/property-type";
import { CardTitleBar } from "@/components/card-sections";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { EmptyValue } from "@/components/catalog/property-value";
import { Code } from "@/components/code";
import { tableCellClass } from "@/components/data-table";
import { Detail, DetailList } from "@/components/detail-list";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
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
import { type IntegrationSummary, integrations } from "@/features/integrations";
import { formatAbsolute, formatRelative } from "@/lib/format";

const sources: Record<ReturnType<typeof propertySource>, { label: string; icon: LucideIcon }> = {
  stored: { label: "Stored", icon: Database },
  relation: { label: "Relation", icon: Link2 },
  lookup: { label: "Lookup", icon: Repeat2 },
  compute: { label: "Computed", icon: Calculator },
  scorecard: { label: "Scorecard", icon: ShieldCheck },
};

// BlueprintOverview shows a blueprint read-only: what each property holds,
// the rules values must follow, and where they come from.
export function BlueprintOverview({
  spec,
  blueprint,
}: Readonly<{ spec: BlueprintSpec; blueprint: Blueprint }>) {
  const usage = useQuery(usageQueryOptions(spec.identifier)).data;

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="gap-0 pb-0">
        <CardTitleBar title="Properties" />
        <CardContent className="px-0">
          <PropertiesTable properties={spec.properties} />
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <AboutCard spec={spec} blueprint={blueprint} usage={usage} />
        <WritersCard spec={spec} usage={usage} />
      </div>
    </div>
  );
}

function PropertiesTable({ properties }: Readonly<{ properties: Property[] }>) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className={tableCellClass}>Property</TableHead>
          <TableHead className={tableCellClass}>Type</TableHead>
          <TableHead className={tableCellClass}>Rules</TableHead>
          <TableHead className={tableCellClass}>Source</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {builtIns.map((builtIn) => (
          <TableRow key={builtIn.identifier} className="align-top text-muted-foreground">
            <TableCell className={cn(tableCellClass, "whitespace-normal")}>
              <div className="grid gap-0.5">
                <span className="font-medium">{builtIn.title}</span>
                <code className="font-mono text-xs">{builtIn.identifier}</code>
              </div>
            </TableCell>
            <TableCell className={tableCellClass}>
              <Badge variant="secondary" className="font-mono">
                {builtIn.type}
              </Badge>
            </TableCell>
            <TableCell className={cn(tableCellClass, "whitespace-normal")}>
              {builtIn.rule}
            </TableCell>
            <TableCell className={tableCellClass}>
              <span className="flex items-center gap-1.5 text-xs">
                <Lock className="size-3.5" aria-hidden />
                Built-in
              </span>
            </TableCell>
          </TableRow>
        ))}
        {properties.map((property) => (
          <TableRow key={property.identifier} className="align-top">
            <TableCell className={cn(tableCellClass, "whitespace-normal")}>
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
            <TableCell className={tableCellClass}>
              <PropertyType property={property} />
            </TableCell>
            <TableCell className={cn(tableCellClass, "whitespace-normal")}>
              <Rules property={property} />
            </TableCell>
            <TableCell className={cn(tableCellClass, "whitespace-normal")}>
              <Source property={property} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

// Every entity has these, whatever its blueprint says, so no property can
// take their names. Paths, filters and jq read them like properties.
const builtIns = [
  { identifier: "identifier", title: "Identifier", type: "string", rule: "Unique, required" },
  { identifier: "title", title: "Title", type: "string", rule: "Defaults to the identifier" },
  { identifier: "blueprint", title: "Blueprint", type: "string", rule: "This blueprint" },
  { identifier: "integration", title: "Integration", type: "string", rule: "Its writer" },
  { identifier: "createdAt", title: "Created", type: "date-time", rule: "Set when written" },
  { identifier: "updatedAt", title: "Updated", type: "date-time", rule: "Set when changed" },
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
    if (min !== undefined && max !== undefined) return `${min}-${max}${unit}`;
    if (min !== undefined) return `≥ ${min}${unit}`;
    if (max !== undefined) return `≤ ${max}${unit}`;
  };
  const length = range(property.minLength, property.maxLength, " chars");
  if (length) rules.push(<RuleText key="length">{length}</RuleText>);
  const bounds = range(property.minimum, property.maximum);
  if (bounds) rules.push(<RuleText key="bounds">{bounds}</RuleText>);
  if (property.pattern) {
    rules.push(<Code key="pattern">/{property.pattern}/</Code>);
  }

  if (rules.length === 0) return <EmptyValue />;
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
              <Code className="block max-w-56 truncate break-normal">{detail}</Code>
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
      <CardTitleBar title="About" />
      <CardContent className="grid gap-4 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <DetailList>
          <Detail label="Entities">
            <span className="font-medium tabular-nums">{usage?.entities ?? "-"}</span>
          </Detail>
          {published && (
            <>
              <Detail label="Version">
                <span className="font-medium tabular-nums">{published.version}</span>
              </Detail>
              <Detail label="Published">
                <span title={formatAbsolute(published.publishedAt)}>
                  {formatRelative(published.publishedAt)}
                </span>
              </Detail>
              {published.publishedBy && <Detail label="By">{published.publishedBy}</Detail>}
            </>
          )}
        </DetailList>
      </CardContent>
    </Card>
  );
}

// WritersCard lists the integrations that write the blueprint's entities.
// Each entity has exactly one writer, so this is where its data comes from.
// WriterIcon shows an integration's tile, its connector's icon.
function WriterIcon({ integration }: Readonly<{ integration?: IntegrationSummary }>) {
  return (
    <span className="mt-0.5">
      <IconTile icon={integration?.icon} color={integration?.color} size="sm" />
    </span>
  );
}

function WritersCard({ spec, usage }: Readonly<{ spec: BlueprintSpec; usage?: Usage }>) {
  const writers = usage?.writers ?? [];
  const { data: integrationList } = useQuery(integrations.queries.list);

  return (
    <Card>
      <CardTitleBar title="Written By" />
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
              <li key={writer.integration} className="flex items-start gap-3 text-sm">
                <WriterIcon
                  integration={integrationList?.find((i) => i.identifier === writer.integration)}
                />
                <div className="grid min-w-0 flex-1">
                  <Link
                    to="/integrations/$identifier"
                    params={{ identifier: writer.integration }}
                    className="truncate font-medium hover:underline"
                  >
                    {writer.title}
                  </Link>
                  <span
                    className="text-xs text-muted-foreground"
                    title={writer.lastRunAt ? formatAbsolute(writer.lastRunAt) : undefined}
                  >
                    {writer.lastRunAt ? formatRelative(writer.lastRunAt) : "Never pushed"}
                  </span>
                </div>
                <span
                  className="font-medium tabular-nums"
                  title={`${writer.entities} ${writer.entities === 1 ? "entity" : "entities"}`}
                >
                  {writer.entities}
                </span>
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
