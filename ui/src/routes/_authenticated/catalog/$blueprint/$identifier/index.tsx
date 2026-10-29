import { createFileRoute } from "@tanstack/react-router";
import { EntityView } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/$identifier/")({
  component: EntityPage,
});

function EntityPage() {
  const { blueprint, identifier } = Route.useParams();
  return (
    <EntityView
      key={`${blueprint}/${identifier}`}
      blueprintId={blueprint}
      identifier={identifier}
    />
  );
}
