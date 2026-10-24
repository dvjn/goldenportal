import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Check, Copy } from "lucide-react";
import { Suspense, useState } from "react";
import { IconButton } from "@/components/icon-button";
import { IngestionTokens } from "@/components/integrations/ingestion-tokens";
import { RecentRuns } from "@/components/integrations/recent-runs";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  connectorsQueryOptions,
  type IntegrationSpec,
  ingestUrl,
  type Mapping,
} from "@/features/integrations";

// IntegrationOverview shows an integration read-only: its connector, how to
// push to it, how data becomes entities, and what recent pushes did.
export function IntegrationOverview({ spec }: Readonly<{ spec: IntegrationSpec }>) {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="grid min-w-0 gap-4">
        <Card>
          <CardHeader className="border-b">
            <CardTitle>Mappings</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4">
            {spec.mappings.map((mapping, index) => (
              // Two mappings can write the same blueprint, so the index keeps keys apart.
              // biome-ignore lint/suspicious/noArrayIndexKey: mappings have no identity of their own.
              <MappingView key={index} mapping={mapping} />
            ))}
          </CardContent>
        </Card>
        <Card className="gap-0 pb-0">
          <CardHeader className="border-b">
            <CardTitle>Recent Runs</CardTitle>
          </CardHeader>
          <CardContent className="px-0">
            <Suspense fallback={null}>
              <RecentRuns identifier={spec.identifier} />
            </Suspense>
          </CardContent>
        </Card>
      </div>
      <div className="grid gap-4">
        <ConnectorCard connector={spec.connector} description={spec.description} />
        <Card>
          <CardHeader className="border-b">
            <CardTitle>Push Endpoint</CardTitle>
          </CardHeader>
          <CardContent className="grid min-w-0 gap-2 text-sm">
            <CopyField value={ingestUrl(spec.identifier)} label="Push endpoint" />
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
          <CardHeader className="border-b">
            <CardTitle>Ingestion Tokens</CardTitle>
          </CardHeader>
          <CardContent>
            <Suspense fallback={null}>
              <IngestionTokens identifier={spec.identifier} />
            </Suspense>
          </CardContent>
        </Card>
      </div>
    </div>
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
      <CardHeader className="border-b">
        <CardTitle>About</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm">
        {description && <p className="text-muted-foreground">{description}</p>}
        <div className="flex items-center gap-2">
          <span className="font-medium">{known?.title ?? connector} Connector</span>
          {known && <Badge variant="secondary">{known.mode}</Badge>}
        </div>
        {known && <p className="text-muted-foreground">{known.description}</p>}
      </CardContent>
    </Card>
  );
}

function MappingView({ mapping }: Readonly<{ mapping: Mapping }>) {
  const rows: [string, string | undefined][] = [
    ["Items", mapping.items],
    ["Filter", mapping.filter],
    ["Identifier", mapping.identifier],
    ["Title", mapping.title],
  ];

  return (
    <section className="grid gap-3 rounded-md border p-3">
      <h3 className="flex items-center gap-2 text-sm font-medium">
        <ArrowRight className="size-4 text-muted-foreground" aria-hidden />
        <Link
          to="/blueprints/$identifier"
          params={{ identifier: mapping.blueprint }}
          className="font-mono hover:underline"
        >
          {mapping.blueprint}
        </Link>
      </h3>
      <dl className="grid grid-cols-[8rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-sm">
        {rows
          .filter(([, value]) => value)
          .map(([label, value]) => (
            <Expression key={label} label={label} value={value ?? ""} />
          ))}
        {mapping.properties.map((field) => (
          <Expression
            key={field.identifier}
            label={field.identifier}
            value={field.expression}
            mono
          />
        ))}
      </dl>
    </section>
  );
}

function Expression({
  label,
  value,
  mono,
}: Readonly<{ label: string; value: string; mono?: boolean }>) {
  return (
    <>
      <dt className={mono ? "truncate font-mono text-xs leading-5" : "text-muted-foreground"}>
        {label}
      </dt>
      <dd>
        <code className="rounded bg-muted px-1 font-mono text-xs break-all">{value}</code>
      </dd>
    </>
  );
}

function CopyField({ value, label }: Readonly<{ value: string; label: string }>) {
  const [copied, setCopied] = useState(false);

  return (
    <div className="flex min-w-0 items-center gap-1 rounded-md bg-muted py-1 pr-1 pl-2">
      <code className="min-w-0 flex-1 truncate font-mono text-xs">{value}</code>
      <IconButton
        label={copied ? "Copied" : `Copy ${label.toLowerCase()}`}
        onClick={async () => {
          await navigator.clipboard.writeText(value);
          setCopied(true);
        }}
      >
        {copied ? <Check /> : <Copy />}
      </IconButton>
    </div>
  );
}
