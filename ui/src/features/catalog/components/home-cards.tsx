import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { SourceIcon } from "@/components/common/source-icon";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { IntegrationIcon } from "@/features/configuration/components/icons";
import { RunStatusIcon } from "@/features/configuration/components/run-status-icon";
import { scheduleLabel } from "@/features/configuration/format";
import type { IntegrationSummary } from "@/features/integrations/api";
import { integrationsQueryOptions } from "@/features/integrations/queries";
import { formatRelative } from "@/lib/time";
import type { Change } from "../api";
import { blueprintIcon } from "../icons";
import { changesQueryOptions, useSourceIcons } from "../queries";
import { Timestamp } from "./property-value";
import { LeafTitle } from "./split-title";

type CardQuery<Data> = { data: Data | undefined; isError: boolean; refetch: () => unknown };

function HomeCard<Data extends unknown[]>({
  title,
  query,
  emptyMessage,
  children,
}: Readonly<{
  title: string;
  query: CardQuery<Data>;
  emptyMessage: string;
  children: (data: Data) => ReactNode;
}>) {
  let body: ReactNode;
  if (query.isError) {
    body = (
      <div className="flex flex-col items-start gap-3 text-sm text-muted-foreground">
        <p>We could not load this card.</p>
        <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </div>
    );
  } else if (query.data === undefined) {
    body = (
      <div className="space-y-3" aria-busy="true">
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-5 w-full" />
        <Skeleton className="h-5 w-4/5" />
      </div>
    );
  } else if (query.data.length === 0) {
    body = <p className="text-sm text-muted-foreground">{emptyMessage}</p>;
  } else {
    body = children(query.data);
  }

  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent className="flex-1">{body}</CardContent>
    </Card>
  );
}

function lastRunNote({ last_run: lastRun }: IntegrationSummary): string {
  if (!lastRun) return "Never run";
  if (lastRun.status === "running") return "Running now";
  return `Finished ${formatRelative(lastRun.finished_at ?? lastRun.started_at)}`;
}

function IntegrationRow({ integration }: Readonly<{ integration: IntegrationSummary }>) {
  return (
    <li>
      <Link
        to="/settings/catalog-setup"
        search={{ tab: "integrations", integration: integration.identifier }}
        data-failed={integration.last_run?.status === "failed"}
        className="flex items-start justify-between gap-4 rounded-lg border border-transparent px-3 py-2.5 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring data-[failed=true]:border-destructive/50 data-[failed=true]:bg-destructive/10"
      >
        <span className="flex min-w-0 items-start gap-3">
          <IntegrationIcon icon={integration.icon} className="mt-0.5 size-5" />
          <span className="min-w-0 space-y-0.5">
            <span className="block truncate font-medium">{integration.title}</span>
            <span className="block text-xs text-muted-foreground">
              {scheduleLabel(integration)}
            </span>
          </span>
        </span>
        <span className="flex shrink-0 flex-col items-end gap-1">
          {integration.last_run ? <RunStatusIcon status={integration.last_run.status} /> : null}
          <span className="text-xs text-muted-foreground">{lastRunNote(integration)}</span>
        </span>
      </Link>
    </li>
  );
}

export function IntegrationsCard() {
  const query = useQuery({
    ...integrationsQueryOptions,
    select: (integrations) => integrations.filter((integration) => !integration.manual),
  });

  return (
    <HomeCard title="Integrations" query={query} emptyMessage="No integrations yet.">
      {(integrations) => (
        <ul className="space-y-1">
          {integrations.map((integration) => (
            <IntegrationRow key={integration.identifier} integration={integration} />
          ))}
        </ul>
      )}
    </HomeCard>
  );
}

function ChangeRow({ change, sourceIcon }: Readonly<{ change: Change; sourceIcon: string }>) {
  return (
    <li className="flex items-start gap-3 text-sm">
      <SourceIcon
        icon={sourceIcon}
        fallback={blueprintIcon(change.blueprint.icon)}
        className="mt-0.5 size-4 text-muted-foreground"
      />
      <div className="min-w-0 flex-1">
        <Link
          to="/catalog/$blueprint/$identifier"
          params={{ blueprint: change.blueprint.identifier, identifier: change.identifier }}
          className="block truncate font-medium hover:underline"
        >
          <LeafTitle title={change.title || change.identifier} />
        </Link>
        <div className="text-xs text-muted-foreground">{change.blueprint.title}</div>
      </div>
      <span className="shrink-0 text-xs whitespace-nowrap text-muted-foreground">
        <Timestamp value={change.updated_at} />
      </span>
    </li>
  );
}

export function RecentChangesCard() {
  const query = useQuery(changesQueryOptions(8));
  const sources = useSourceIcons().data;

  return (
    <HomeCard title="Recent changes" query={query} emptyMessage="No changes yet.">
      {(changes) => (
        <ul className="space-y-3">
          {changes.map((change) => (
            <ChangeRow
              key={`${change.blueprint.identifier}/${change.identifier}`}
              change={change}
              sourceIcon={sources?.ofIntegration(change.integration.identifier) ?? ""}
            />
          ))}
        </ul>
      )}
    </HomeCard>
  );
}
