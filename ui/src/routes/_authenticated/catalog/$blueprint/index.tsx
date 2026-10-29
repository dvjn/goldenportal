import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";
import { type CatalogSearch, CatalogView, parseCatalogSearch } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/")({
  validateSearch: (raw: Record<string, unknown>): CatalogSearch => parseCatalogSearch(raw),
  component: EntitiesPage,
});

function EntitiesPage() {
  const { blueprint } = Route.useParams();
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const onSearchChange = useCallback(
    (next: Partial<CatalogSearch>) =>
      void navigate({ search: (previous) => ({ ...previous, ...next }), replace: true }),
    [navigate],
  );

  return <CatalogView blueprintId={blueprint} search={search} onSearchChange={onSearchChange} />;
}
