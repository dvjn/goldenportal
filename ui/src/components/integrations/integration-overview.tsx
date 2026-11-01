import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { Code } from "@/components/code";
import { Detail, DetailList } from "@/components/detail-list";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { blueprints } from "@/features/blueprints";
import {
  connectorsQueryOptions,
  type IntegrationSpec,
  type Mapping,
  statusQueryOptions,
} from "@/features/integrations";

// IntegrationOverview shows an integration read-only: its connector, what
// it writes and how data becomes entities. Pushing has its own tab, and
// syncing sits in the page header.
export function IntegrationOverview({ spec }: Readonly<{ spec: IntegrationSpec }>) {
  const { data: connectors } = useQuery(connectorsQueryOptions);
  const pulls = connectors?.find((c) => c.id === spec.connector.type)?.mode === "pull";

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <Card className="min-w-0 gap-0 pb-0">
        <CardTitleBar title="Mappings" />
        <CardContent className="grid px-0">
          {spec.mappings.map((mapping, index) => (
            // Two mappings can write the same blueprint, so the index keeps keys apart.
            // biome-ignore lint/suspicious/noArrayIndexKey: mappings have no identity of their own.
            <MappingView key={index} mapping={mapping} />
          ))}
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <ConnectorCard
          connector={spec.connector.type}
          description={spec.description}
          interval={pulls ? (spec.sync?.interval ?? "15m") : undefined}
        />
        <FeedsCard identifier={spec.identifier} />
      </div>
    </div>
  );
}

// FeedsCard lists the blueprints an integration writes, with how many of
// their entities it wrote.
function FeedsCard({ identifier }: Readonly<{ identifier: string }>) {
  const { data } = useQuery(statusQueryOptions);
  const status = data?.find((s) => s.integration === identifier);
  if (!status) return null;

  return (
    <Card>
      <CardTitleBar title="Feeds" />
      <CardContent>
        {status.feeds.length === 0 ? (
          <p className="text-sm text-muted-foreground">It doesn't write any blueprint yet.</p>
        ) : (
          <ul className="grid gap-2 text-sm">
            {status.feeds.map((feed) => (
              <li key={feed.blueprint} className="flex items-center justify-between gap-3">
                <Link
                  to="/catalog/$blueprint"
                  params={{ blueprint: feed.blueprint }}
                  className="min-w-0 hover:underline"
                >
                  <BlueprintLabel blueprint={feed.blueprint} />
                </Link>
                <span
                  className="font-medium tabular-nums"
                  title={`${feed.entities} ${feed.entities === 1 ? "entity" : "entities"}`}
                >
                  {feed.entities}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

// ConnectorCard says what the integration connects to, and for one that
// pulls, how often it syncs; each sync is a full snapshot, so entities
// that drop out of it are deleted.
function ConnectorCard({
  connector,
  description,
  interval,
}: Readonly<{ connector: string; description?: string; interval?: string }>) {
  const { data: connectors } = useQuery(connectorsQueryOptions);
  const known = connectors?.find((c) => c.id === connector);

  return (
    <Card>
      <CardTitleBar title="About" />
      <CardContent className="grid gap-3 text-sm">
        {description && <p className="text-muted-foreground">{description}</p>}
        <div className="flex items-center gap-2">
          <IconTile icon={known?.icon} color={known?.color} size="sm" />
          <span className="font-medium">{known?.title ?? connector} Connector</span>
          {known && <Badge variant="secondary">{known.mode}</Badge>}
        </div>
        {known && <p className="text-muted-foreground">{known.description}</p>}
        {interval && (
          <DetailList>
            <Detail label="Syncs Every">
              <code className="font-mono">{interval}</code>
            </Detail>
          </DetailList>
        )}
      </CardContent>
    </Card>
  );
}

function MappingView({ mapping }: Readonly<{ mapping: Mapping }>) {
  const { data: target } = useQuery(blueprints.queries.detail(mapping.blueprint));
  const properties = (target?.published?.spec ?? target?.draft?.spec)?.properties ?? [];
  const rows: [string, string | undefined][] = [
    ["Items", mapping.items === "." ? undefined : mapping.items],
    ["Filter", mapping.filter],
    ["Identifier", mapping.entity.identifier],
    ["Title", mapping.entity.title],
    ...mapping.entity.properties.map((field): [string, string] => [
      properties.find((p) => p.identifier === field.identifier)?.title ?? field.identifier,
      field.expression,
    ]),
  ];

  return (
    <CardSection
      heading={
        <>
          {mapping.resource && (
            <>
              <code className="font-mono text-muted-foreground">{mapping.resource}</code>
              <ArrowRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            </>
          )}
          <Link
            to="/blueprints/$identifier"
            params={{ identifier: mapping.blueprint }}
            className="min-w-0 hover:underline"
          >
            <BlueprintLabel blueprint={mapping.blueprint} />
          </Link>
        </>
      }
    >
      <DetailList>
        {rows
          .filter(([, value]) => value)
          .map(([label, value]) => (
            <Detail key={label} label={label}>
              <Code>{value}</Code>
            </Detail>
          ))}
      </DetailList>
    </CardSection>
  );
}
