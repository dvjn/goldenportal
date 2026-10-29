import { createFileRoute } from "@tanstack/react-router";
import { requirePermission } from "@/features/auth";
import { EditEntityPage } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/$identifier/edit")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.write"),
  component: EditEntityRoute,
});

function EditEntityRoute() {
  const { blueprint, identifier } = Route.useParams();
  return <EditEntityPage blueprintId={blueprint} identifier={identifier} />;
}
