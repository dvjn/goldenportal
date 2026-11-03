import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ConciergeBell, Search } from "lucide-react";
import { useState } from "react";
import { OfferCard } from "@/components/actions/offer-card";
import { RunActionDialog } from "@/components/actions/run-action-dialog";
import { EmptyCard } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { type Offer, selfServiceCatalogQueryOptions } from "@/features/actions";

// SelfServiceSearch is the page's state in the URL: what's searched for,
// and the workflow or action whose form is open, so a search result can
// open one.
type SelfServiceSearch = { q?: string; workflow?: string; action?: string };

function text(value: unknown) {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export const Route = createFileRoute("/_app/self-service/")({
  validateSearch: (search: Record<string, unknown>): SelfServiceSearch => ({
    ...(text(search.q) ? { q: text(search.q) } : {}),
    ...(text(search.workflow) ? { workflow: text(search.workflow) } : {}),
    ...(text(search.action) ? { action: text(search.action) } : {}),
  }),
  loader: ({ context }) => context.queryClient.ensureQueryData(selfServiceCatalogQueryOptions),
  component: SelfServicePage,
});

function matches(offer: Offer, q: string) {
  const needle = q.trim().toLowerCase();
  return [offer.title, offer.identifier, offer.description ?? "", offer.blueprintTitle ?? ""].some(
    (field) => field.toLowerCase().includes(needle),
  );
}

const sectionHeading = "text-xs font-medium tracking-wide text-muted-foreground uppercase";

// SelfServicePage is what people can run, by group as the server orders
// them: workflows, and actions, whose forms ask for the entity first.
function SelfServicePage() {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: catalog } = useSuspenseQuery(selfServiceCatalogQueryOptions);
  const [query, setQuery] = useState(search.q ?? "");
  const q = query.trim();
  const everything = catalog.groups.flatMap((group) => group.items);
  const groups = catalog.groups
    .map((group) => ({ ...group, items: group.items.filter((offer) => matches(offer, q)) }))
    .filter((group) => group.items.length > 0);

  function searchFor(next: string) {
    setQuery(next);
    void navigate({ search: (prev) => ({ ...prev, q: text(next) }), replace: true });
  }
  const open = (patch: Pick<SelfServiceSearch, "workflow" | "action">) =>
    navigate({
      search: (prev) => ({ ...prev, workflow: undefined, action: undefined, ...patch }),
    });
  const picked = everything.find((offer) =>
    offer.kind === "workflow"
      ? offer.identifier === search.workflow
      : offer.identifier === search.action,
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Self-Service" icon={<PageIcon page="selfService" />} />
      {everything.length === 0 ? (
        <EmptyCard
          icon={<ConciergeBell />}
          title="Nothing to run yet"
          description="What builders offer to run shows up here."
        />
      ) : (
        <>
          <InputGroup className="max-w-3xl">
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
            <InputGroupInput
              type="search"
              value={query}
              onChange={(event) => searchFor(event.target.value)}
              placeholder="Search self-service"
              aria-label="Search self-service"
            />
          </InputGroup>
          {groups.length === 0 && (
            <p className="text-sm text-muted-foreground">
              Nothing matches "{q}".{" "}
              <Button variant="link" className="h-auto p-0" onClick={() => searchFor("")}>
                Clear the search
              </Button>
            </p>
          )}
          {groups.map((group) => (
            <section key={group.title} className="grid gap-3" aria-label={group.title}>
              <h2 className={sectionHeading}>{group.title}</h2>
              <ul className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                {group.items.map((offer) => (
                  <li key={`${offer.kind}:${offer.identifier}`}>
                    <OfferCard
                      offer={offer}
                      onOpen={() =>
                        open(
                          offer.kind === "workflow"
                            ? { workflow: offer.identifier }
                            : { action: offer.identifier },
                        )
                      }
                    />
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </>
      )}
      {picked && (
        <RunActionDialog
          // A new pick starts a new form.
          key={`${picked.kind}:${picked.identifier}`}
          action={picked}
          kind={picked.kind}
          blueprint={picked.blueprint}
          open
          onOpenChange={(next) => {
            if (!next) void open({});
          }}
        />
      )}
    </div>
  );
}
