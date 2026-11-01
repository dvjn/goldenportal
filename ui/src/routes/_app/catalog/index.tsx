import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { LayoutGrid, Search } from "lucide-react";
import { useState } from "react";
import { EmptyCard } from "@/components/empty-state";
import { LinkCard } from "@/components/link-card";
import { PageHeader } from "@/components/page-header";
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
        <EmptyCard
          icon={<LayoutGrid />}
          title="Nothing in the catalog yet"
          description="Blueprints show up here once a builder publishes them."
        />
      ) : shown.length === 0 ? (
        <p className="text-sm text-muted-foreground">No blueprint matches "{query}".</p>
      ) : (
        // Cards share their rows, so counts line up across a row.
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((entry) => (
            <li key={entry.identifier} className="row-span-2 grid grid-rows-subgrid">
              <LinkCard
                to="/catalog/$blueprint"
                params={{ blueprint: entry.identifier }}
                icon={entry.icon}
                color={entry.color}
                title={entry.title}
                description={entry.description}
                className="row-span-2 grid grid-rows-subgrid gap-y-3"
              >
                <p
                  className="flex items-baseline gap-1.5 self-end px-4"
                  title={`${entry.entities} entities`}
                >
                  <span className="text-2xl font-semibold tabular-nums">
                    {count.format(entry.entities)}
                  </span>
                  <span className="text-sm text-muted-foreground">
                    {entry.entities === 1 ? "entity" : "entities"}
                  </span>
                </p>
              </LinkCard>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
