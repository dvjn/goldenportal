import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Loader2, RefreshCw } from "lucide-react";
import { Suspense } from "react";
import { toast } from "sonner";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { Code } from "@/components/code";
import { CopyIconButton } from "@/components/copy-button";
import { Detail, DetailList } from "@/components/detail-list";
import { IngestionTokens } from "@/components/integrations/ingestion-tokens";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { blueprints } from "@/features/blueprints";
import {
  connectorsQueryOptions,
  type IntegrationSpec,
  ingestUrl,
  integrationKeys,
  type Mapping,
  statusQueryOptions,
  syncIntegration,
} from "@/features/integrations";

// IntegrationOverview shows an integration read-only: its connector, how
// data reaches it and how data becomes entities.
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
        <ConnectorCard connector={spec.connector.type} description={spec.description} />
        <FeedsCard identifier={spec.identifier} />
        {spec.connector.type === "http" && <ResourcesCard config={spec.connector.config} />}
        {pulls ? (
          <SyncCard identifier={spec.identifier} interval={spec.sync?.interval ?? "15m"} />
        ) : (
          <PushCards identifier={spec.identifier} />
        )}
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

// PushCards show how other systems push: the endpoint and the tokens.
function PushCards({ identifier }: Readonly<{ identifier: string }>) {
  return (
    <>
      <Card>
        <CardTitleBar title="Push Endpoint" />
        <CardContent className="grid min-w-0 gap-2 text-sm">
          <CopyField value={ingestUrl(identifier)} label="Push endpoint" />
          <p className="text-muted-foreground">
            <code className="font-mono">POST</code> JSON with an ingestion token.{" "}
            <code className="font-mono">
              DELETE …/entities/&lt;blueprint&gt;/&lt;identifier&gt;
            </code>{" "}
            removes one.
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardTitleBar title="Ingestion Tokens" />
        <CardContent>
          <Suspense fallback={null}>
            <IngestionTokens identifier={identifier} />
          </Suspense>
        </CardContent>
      </Card>
    </>
  );
}

type HTTPResource = {
  url?: string;
  next?: string;
  headers?: Record<string, string | { env?: string }>;
};

// ResourcesCard lists what an http integration fetches. Headers show by
// name only, and secrets by the variable they're read from.
function ResourcesCard({ config }: Readonly<{ config?: Record<string, unknown> }>) {
  const resources = (config?.resources ?? {}) as Record<string, HTTPResource>;

  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Resources" />
      <CardContent className="grid min-w-0 px-0 text-sm">
        {Object.entries(resources).map(([name, resource]) => (
          <CardSection
            key={name}
            className="min-w-0 gap-1.5"
            heading={<code className="font-mono text-xs">{name}</code>}
          >
            <Code className="truncate py-1 break-normal" title={resource.url}>
              GET {resource.url}
            </Code>
            {resource.next && (
              <span className="text-xs text-muted-foreground">
                Next page: <code className="font-mono">{resource.next}</code>
              </span>
            )}
            {Object.entries(resource.headers ?? {}).map(([header, value]) => (
              <span key={header} className="text-xs text-muted-foreground">
                {header}:{" "}
                {typeof value === "object" && value.env ? (
                  <code className="font-mono">${value.env}</code>
                ) : (
                  "set"
                )}
              </span>
            ))}
          </CardSection>
        ))}
      </CardContent>
    </Card>
  );
}

// SyncCard syncs a pull integration now, on top of its schedule.
function SyncCard({ identifier, interval }: Readonly<{ identifier: string; interval: string }>) {
  const queryClient = useQueryClient();
  const sync = useMutation({
    mutationFn: () => syncIntegration(identifier),
    // A failed sync is recorded as a run too, so both outcomes refresh the runs.
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: integrationKeys.runs(identifier) }),
        queryClient.invalidateQueries({ queryKey: integrationKeys.status }),
      ]),
    onSuccess: (run) => {
      toast.success(
        `Synced: ${run.created} created, ${run.updated} updated, ${run.deleted} deleted` +
          (run.failed > 0 ? `, ${run.failed} failed` : ""),
      );
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <Card>
      <CardTitleBar title="Sync" />
      <CardContent className="grid gap-3 text-sm">
        <p className="text-muted-foreground">
          Syncs every <code className="font-mono">{interval}</code>. Each sync is a full snapshot,
          so entities that drop out of it are deleted.
        </p>
        <Button
          variant="outline"
          size="sm"
          className="justify-self-start"
          onClick={() => sync.mutate()}
          disabled={sync.isPending}
        >
          {sync.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          Sync Now
        </Button>
      </CardContent>
    </Card>
  );
}

function ConnectorCard({
  connector,
  description,
}: Readonly<{ connector: string; description?: string }>) {
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

function CopyField({ value, label }: Readonly<{ value: string; label: string }>) {
  return (
    <div className="flex min-w-0 items-center gap-1 rounded-md bg-muted py-1 pr-1 pl-2">
      <code className="min-w-0 flex-1 truncate font-mono text-xs">{value}</code>
      <CopyIconButton value={value} label={label.toLowerCase()} />
    </div>
  );
}
