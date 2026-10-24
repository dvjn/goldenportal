import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { LayoutGrid, Search } from "lucide-react";
import { useState } from "react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { PageHeader } from "@/components/page-header";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { catalogQueryOptions } from "@/features/catalog";

export const Route = createFileRoute("/_app/catalog/")({
  loader: ({ context }) => context.queryClient.ensureQueryData(catalogQueryOptions),
  component: CatalogPage,
});

const count = new Intl.NumberFormat("en", { notation: "compact" });

function CatalogPage() {
  const { data: entries } = useSuspenseQuery(catalogQueryOptions);
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const shown = entries.filter((entry) =>
    [entry.title, entry.identifier, entry.description ?? ""].some((text) =>
      text.toLowerCase().includes(needle),
    ),
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Catalog"
        description="Everything the portal knows about, by blueprint."
        actions={
          entries.length > 0 && (
            <div className="relative w-64">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="search"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Filter blueprints"
                aria-label="Filter blueprints"
                className="pl-8"
              />
            </div>
          )
        }
      />
      {entries.length === 0 ? (
        <Card className="py-0">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <LayoutGrid />
              </EmptyMedia>
              <EmptyTitle>Nothing in the catalog yet</EmptyTitle>
              <EmptyDescription>
                Blueprints show up here once a builder publishes them.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </Card>
      ) : shown.length === 0 ? (
        <p className="text-sm text-muted-foreground">No blueprint matches "{query}".</p>
      ) : (
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((entry) => (
            <li key={entry.identifier}>
              <Link
                to="/catalog/$blueprint"
                params={{ blueprint: entry.identifier }}
                className="block h-full rounded-xl focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
              >
                <Card className="h-full transition-colors hover:bg-accent/50">
                  <CardHeader>
                    <div className="flex items-start justify-between gap-3">
                      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                        <BlueprintIcon icon={entry.icon} className="size-4.5" />
                      </span>
                      <span
                        className="text-2xl font-semibold tabular-nums"
                        title={`${entry.entities} entities`}
                      >
                        {count.format(entry.entities)}
                      </span>
                    </div>
                    <CardTitle className="pt-2">{entry.title}</CardTitle>
                    {entry.description && <CardDescription>{entry.description}</CardDescription>}
                  </CardHeader>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
