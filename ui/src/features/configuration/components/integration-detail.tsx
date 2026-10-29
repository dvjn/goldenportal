import { Link } from "@tanstack/react-router";
import { ArrowRightIcon } from "lucide-react";
import { DescriptionList, DescriptionRow } from "@/components/common/description-list";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { StateCard } from "@/components/common/state-card";
import { TableFrame } from "@/components/common/table-frame";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useCan } from "@/features/auth";
import {
  type IntegrationDetail as Detail,
  type FieldMapping,
  type IntegrationMapping,
  useIntegration,
} from "@/features/integrations";
import { entityCountLabel, scheduleLabel } from "../format";
import { ConfigValue } from "./config-value";
import { ConnectorLabel, IntegrationIcon } from "./icons";
import { IngestTokens } from "./ingest-tokens";
import { DetailHeader, DetailSection } from "./master-detail";
import { PushInstructions } from "./push-instructions";
import { RunHistory } from "./run-history";
import { SyncButton } from "./sync-button";

type MappingRow = {
  key: string;
  field: string;
  group: "identifier" | "title" | "property" | "relation";
  expression: string;
  target: string;
};

function propertyTarget(field: FieldMapping) {
  return field.format ? `${field.type}, ${field.format}` : field.type;
}

function relationTarget(field: FieldMapping) {
  return `${field.target ?? ""} (${field.many ? "many" : "one"})`;
}

function mappingRows(mapping: IntegrationMapping): MappingRow[] {
  return [
    {
      key: "identifier",
      field: "identifier",
      group: "identifier" as const,
      expression: mapping.identifier ?? "",
      target: "string",
    },
    {
      key: "title",
      field: "title",
      group: "title" as const,
      expression: mapping.title ?? "",
      target: "string",
    },
    ...mapping.properties.map((property) => ({
      key: `property.${property.key}`,
      field: property.key,
      group: "property" as const,
      expression: property.expression,
      target: propertyTarget(property),
    })),
    ...mapping.relations.map((relation) => ({
      key: `relation.${relation.key}`,
      field: relation.key,
      group: "relation" as const,
      expression: relation.expression,
      target: relationTarget(relation),
    })),
  ].filter((row) => row.expression !== "");
}

function KindBadge({ connector, kind }: Readonly<{ connector: string; kind: string }>) {
  return (
    <Badge variant="outline" className="font-mono">
      {connector} {kind}
    </Badge>
  );
}

function feeds(integration: Detail) {
  const kindsByBlueprint = new Map<string, { title: string; kinds: string[] }>();
  for (const kind of integration.kinds) {
    const current = kindsByBlueprint.get(kind.blueprint);
    kindsByBlueprint.set(kind.blueprint, {
      title: kind.blueprint_title,
      kinds: [...(current?.kinds ?? []), kind.kind],
    });
  }
  return [...kindsByBlueprint].map(([blueprint, feed]) => ({
    blueprint,
    ...feed,
    count: integration.entities.find((group) => group.blueprint === blueprint)?.count ?? 0,
  }));
}

function MappingTable({
  connector,
  mapping,
}: Readonly<{ connector: string; mapping: IntegrationMapping }>) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <KindBadge connector={connector} kind={mapping.kind} />
        <ArrowRightIcon className="size-4 text-muted-foreground" aria-hidden="true" />
        <span className="font-medium">{mapping.blueprint_title}</span>
        {mapping.lookups.length > 0 ? (
          <span className="text-muted-foreground">
            looks up{" "}
            {mapping.lookups.map((lookup) => (
              <code key={lookup} className="mr-1 font-mono text-xs">
                ${lookup.replaceAll("-", "_")}
              </code>
            ))}
          </span>
        ) : null}
      </div>
      {mapping.canonical ? (
        <p className="text-sm text-muted-foreground">
          Records arrive as entities, with identifier, title, properties and relations.
        </p>
      ) : (
        <TableFrame>
          <TableHeader>
            <TableRow>
              <TableHead>Field</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Expression</TableHead>
              <TableHead>Target type</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {mappingRows(mapping).map((row) => (
              <TableRow key={row.key}>
                <TableCell className="font-mono text-xs">{row.field}</TableCell>
                <TableCell className="text-muted-foreground">{row.group}</TableCell>
                <TableCell className="font-mono text-xs break-all whitespace-pre-wrap">
                  {row.expression}
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {row.target}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </TableFrame>
      )}
    </div>
  );
}

function SourceLink({ document }: Readonly<{ document: string }>) {
  const can = useCan();
  if (!can("catalog.model")) return <code className="font-mono text-xs">{document}</code>;
  return (
    <Link
      to="/settings/catalog-setup"
      search={{ tab: "definitions", document }}
      className="font-mono text-xs underline-offset-4 hover:underline"
    >
      {document}
    </Link>
  );
}

function IntegrationContent({ integration }: Readonly<{ integration: Detail }>) {
  const can = useCan();
  const running = integration.last_run?.status === "running";
  const push = integration.connector === "push";

  return (
    <div className="space-y-8 p-5">
      <div className="space-y-1">
        <DetailHeader
          icon={<IntegrationIcon icon={integration.icon} className="size-6" />}
          title={integration.title}
          meta={
            <>
              <Badge variant="outline" className="font-mono">
                {integration.identifier}
              </Badge>
              <span className="text-sm">
                <ConnectorLabel connector={integration.connector} />
              </span>
              <span className="text-sm text-muted-foreground">{scheduleLabel(integration)}</span>
            </>
          }
          actions={
            integration.pull && can("integrations.sync") ? (
              <SyncButton identifier={integration.identifier} running={running} />
            ) : null
          }
        />
        {integration.description ? (
          <p className="text-sm text-muted-foreground">{integration.description}</p>
        ) : null}
      </div>

      <DetailSection title="Feeds">
        {integration.kinds.length === 0 ? (
          <p className="text-sm text-muted-foreground">This integration maps no kinds yet.</p>
        ) : (
          <ul className="grid grid-cols-[auto_auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 text-sm">
            {feeds(integration).map((feed) => (
              <li key={feed.blueprint} className="col-span-4 grid grid-cols-subgrid items-center">
                <span className="flex flex-wrap gap-1">
                  {feed.kinds.map((kind) => (
                    <KindBadge key={kind} connector={integration.connector} kind={kind} />
                  ))}
                </span>
                <ArrowRightIcon className="size-4 text-muted-foreground" aria-hidden="true" />
                <span className="truncate">
                  <span className="font-medium">{feed.title}</span>{" "}
                  <span className="text-muted-foreground">{entityCountLabel(feed.count)}</span>
                </span>
                <Button asChild variant="outline" size="sm">
                  <Link to="/catalog/$blueprint" params={{ blueprint: feed.blueprint }}>
                    Open catalog
                  </Link>
                </Button>
              </li>
            ))}
          </ul>
        )}
      </DetailSection>

      <DetailSection title="Settings">
        <DescriptionList>
          <DescriptionRow label="Connector">
            <ConnectorLabel connector={integration.connector} />
          </DescriptionRow>
          {integration.secrets.map((secret) => (
            <DescriptionRow key={secret.name} label={`Secret ${secret.name}`}>
              <span className="inline-flex flex-wrap items-center gap-2">
                <code className="font-mono text-xs">{secret.variable}</code>
                <Badge variant={secret.present ? "success" : "destructive"}>
                  {secret.present ? "Present" : "Missing"}
                </Badge>
              </span>
            </DescriptionRow>
          ))}
          {Object.entries(integration.config).map(([key, value]) => (
            <DescriptionRow key={key} label={key}>
              <ConfigValue value={value} />
            </DescriptionRow>
          ))}
          {integration.document ? (
            <DescriptionRow label="Source">
              <SourceLink document={integration.document} />
            </DescriptionRow>
          ) : null}
        </DescriptionList>
      </DetailSection>

      {push ? (
        <DetailSection title="Push">
          <div className="space-y-6">
            <PushInstructions integration={integration} />
            {can("integrations.tokens") ? <IngestTokens integration={integration} /> : null}
          </div>
        </DetailSection>
      ) : null}

      {integration.mappings.length > 0 ? (
        <DetailSection title="Mapping">
          <div className="space-y-6">
            {integration.mappings.map((mapping) => (
              <MappingTable
                key={mapping.kind}
                connector={integration.connector}
                mapping={mapping}
              />
            ))}
          </div>
        </DetailSection>
      ) : null}

      <DetailSection title="Runs">
        <RunHistory integration={integration.identifier} />
      </DetailSection>
    </div>
  );
}

export function IntegrationDetail({ identifier }: Readonly<{ identifier: string }>) {
  const integration = useIntegration(identifier);
  if (integration.isError) {
    return (
      <div className="p-5">
        <StateCard
          title="Integration unavailable"
          message="We could not load this integration. Try again."
        />
      </div>
    );
  }
  if (!integration.data) {
    return (
      <div className="p-5">
        <ListSkeleton />
      </div>
    );
  }
  return <IntegrationContent integration={integration.data} />;
}
