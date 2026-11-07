import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { type RunsSearch, RunsTable, validateRunsSearch } from "@/components/actions/runs-table";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import { hasRole, useMe } from "@/features/auth";

// RunsPageSearch is the page's state in the URL: who is "all" once a
// builder removes the Requested By filter, which starts on their own runs,
// and blueprint and entity narrow the runs to one entity's.
type RunsPageSearch = RunsSearch & { who?: "all"; blueprint?: string; entity?: string };

function text(value: unknown) {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function validateRunsPageSearch(search: Record<string, unknown>): RunsPageSearch {
  const blueprint = text(search.blueprint);
  const entity = blueprint ? text(search.entity) : undefined;
  return {
    ...validateRunsSearch(search),
    ...(search.who === "all" ? { who: "all" as const } : {}),
    ...(blueprint && entity ? { blueprint, entity } : {}),
  };
}

export const Route = createFileRoute("/_app/runs/")({
  validateSearch: validateRunsPageSearch,
  component: RunsPage,
});

function RunsPage() {
  const { who, blueprint, entity, ...search } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const builder = hasRole(useMe().role, "builder");
  const everyone = builder && who === "all";

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Runs" icon={<PageIcon page="runs" />} />
      <RunsTable
        scope={{ blueprint, entity, mine: !everyone }}
        search={search}
        hide={everyone ? [] : ["requester"]}
        mine={
          builder
            ? {
                value: !everyone,
                onChange: (mine) =>
                  void navigate({
                    search: (prev) => ({ ...prev, who: mine ? undefined : "all", page: undefined }),
                    replace: true,
                  }),
              }
            : undefined
        }
        fixedFilters={
          blueprint && entity
            ? [
                {
                  title: "Entity",
                  summary: entity,
                  onRemove: () =>
                    void navigate({
                      search: (prev) => ({ ...prev, blueprint: undefined, entity: undefined }),
                      replace: true,
                    }),
                },
              ]
            : undefined
        }
        empty={
          everyone
            ? { title: "No runs yet", description: "Runs of actions show up here." }
            : {
                title: "No runs by you yet",
                description: "Run an action from an entity's page, and it shows up here.",
              }
        }
      />
    </div>
  );
}
