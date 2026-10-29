import { createFileRoute } from "@tanstack/react-router";
import { requirePermission } from "@/features/auth";
import { NewEntityPage } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/new")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.write"),
  component: NewEntityRoute,
});

function NewEntityRoute() {
  const { blueprint } = Route.useParams();
  return <NewEntityPage blueprintId={blueprint} />;
}
