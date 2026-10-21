import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  BlueprintIcon,
  type EntityPage,
  type EntitySearch,
  entitiesQueryOptions,
  IntegrationBadge,
  PropertyValue,
  useBlueprint,
  useEntities,
} from "@/features/catalog";

type EntityListSearch = Partial<EntitySearch>;

function nonEmpty(value: unknown) {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function entitySearch(search: EntityListSearch): EntitySearch {
  return { q: search.q, integration: search.integration, page: search.page ?? 1 };
}

const allIntegrations = "all";
const searchDelayMs = 300;

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/")({
  validateSearch: (search: Record<string, unknown>): EntityListSearch => {
    const page = Math.floor(Number(search.page)) || 1;
    return {
      q: nonEmpty(search.q),
      integration: nonEmpty(search.integration),
      page: page > 1 ? page : undefined,
    };
  },
  loaderDeps: ({ search }) => entitySearch(search),
  loader: ({ context, params, deps }) =>
    context.queryClient.ensureQueryData(entitiesQueryOptions(params.blueprint, deps)),
  component: EntitiesPage,
});

function EntitiesPage() {
  const params = Route.useParams();
  const search = entitySearch(Route.useSearch());
  const blueprint = useBlueprint(params.blueprint);
  const page = useEntities(params.blueprint, search);
  const columns = blueprint.columns.flatMap((key) =>
    blueprint.properties.filter((property) => property.key === key),
  );
  const first = page.total === 0 ? 0 : page.offset + 1;
  const last = page.offset + page.entities.length;

  return (
    <>
      <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            <BlueprintIcon icon={blueprint.icon} className="size-5" />
            {blueprint.title}
          </h1>
          {blueprint.description && (
            <p className="text-muted-foreground text-sm">{blueprint.description}</p>
          )}
        </div>
      </header>
      <div className="flex flex-col gap-2 sm:flex-row">
        <SearchInput />
        <IntegrationFilter integrations={page.integrations} />
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Title</TableHead>
            <TableHead>Identifier</TableHead>
            <TableHead>Integration</TableHead>
            {columns.map((property) => (
              <TableHead key={property.key}>{property.title}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {page.entities.map((entity) => (
            <TableRow key={entity.identifier}>
              <TableCell>
                <Link
                  to="/catalog/$blueprint/$identifier"
                  params={{ blueprint: params.blueprint, identifier: entity.identifier }}
                  className="font-medium underline-offset-4 hover:underline"
                >
                  {entity.title}
                </Link>
              </TableCell>
              <TableCell className="text-muted-foreground font-mono text-xs">
                {entity.identifier}
              </TableCell>
              <TableCell>
                <IntegrationBadge integration={entity.integration} />
              </TableCell>
              {columns.map((property) => (
                <TableCell key={property.key}>
                  <PropertyValue property={property} value={entity.properties[property.key]} />
                </TableCell>
              ))}
            </TableRow>
          ))}
          {page.entities.length === 0 && (
            <TableRow>
              <TableCell colSpan={3 + columns.length} className="text-muted-foreground text-center">
                {search.q || search.integration
                  ? "No entities match the search."
                  : `No ${blueprint.title.toLowerCase()} entities yet.`}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="flex items-center justify-between gap-2">
        <p className="text-muted-foreground text-sm">
          {first} to {last} of {page.total}
        </p>
        <div className="flex gap-2">
          <PageLink page={search.page - 1} disabled={search.page <= 1} label="Previous" />
          <PageLink page={search.page + 1} disabled={last >= page.total} label="Next" />
        </div>
      </div>
    </>
  );
}

function SearchInput() {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const [query, setQuery] = useState(search.q ?? "");

  useEffect(() => {
    if (query === (search.q ?? "")) return;
    const timer = setTimeout(() => {
      navigate({
        search: (previous) => ({ ...previous, q: query || undefined, page: undefined }),
        replace: true,
      });
    }, searchDelayMs);
    return () => clearTimeout(timer);
  }, [query, search.q, navigate]);

  return (
    <InputGroup className="sm:max-w-sm">
      <InputGroupAddon>
        <Search />
      </InputGroupAddon>
      <InputGroupInput
        aria-label="Search by title or identifier"
        placeholder="Search by title or identifier"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
    </InputGroup>
  );
}

function IntegrationFilter({ integrations }: { integrations: EntityPage["integrations"] }) {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });

  return (
    <Select
      value={search.integration ?? allIntegrations}
      onValueChange={(value) =>
        navigate({
          search: (previous) => ({
            ...previous,
            integration: value === allIntegrations ? undefined : value,
            page: undefined,
          }),
        })
      }
    >
      <SelectTrigger aria-label="Filter by integration" className="sm:w-56">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={allIntegrations}>All integrations</SelectItem>
        {integrations.map((integration) => (
          <SelectItem key={integration.identifier} value={integration.identifier}>
            {integration.title} ({integration.count})
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function PageLink({ page, disabled, label }: { page: number; disabled: boolean; label: string }) {
  if (disabled) {
    return (
      <Button variant="outline" size="sm" disabled>
        {label}
      </Button>
    );
  }

  return (
    <Button variant="outline" size="sm" asChild>
      <Link
        from={Route.fullPath}
        search={(previous) => ({ ...previous, page: page > 1 ? page : undefined })}
      >
        {label}
      </Link>
    </Button>
  );
}
