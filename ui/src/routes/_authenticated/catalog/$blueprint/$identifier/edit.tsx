import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { requirePermission } from "@/features/auth";
import { EntityForm, useBlueprint, useEntity } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/$identifier/edit")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.write"),
  staticData: { breadcrumb: "Edit" },
  component: EditEntityPage,
});

function EditEntityPage() {
  const params = Route.useParams();
  const blueprint = useBlueprint(params.blueprint);
  const entity = useEntity(params.blueprint, params.identifier);
  const navigate = useNavigate();
  const back = () => navigate({ to: "/catalog/$blueprint/$identifier", params });

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">Edit {entity.title}</h1>
      </header>
      <EntityForm blueprint={blueprint} entity={entity} onSaved={back} onCancel={back} />
    </>
  );
}
