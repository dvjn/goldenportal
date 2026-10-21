import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { requirePermission } from "@/features/auth";
import { EntityForm, useBlueprint } from "@/features/catalog";

export const Route = createFileRoute("/_authenticated/catalog/$blueprint/new")({
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "catalog.write"),
  staticData: { breadcrumb: "New entity" },
  component: NewEntityPage,
});

function NewEntityPage() {
  const params = Route.useParams();
  const blueprint = useBlueprint(params.blueprint);
  const navigate = useNavigate();

  return (
    <>
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">New {blueprint.title.toLowerCase()}</h1>
        <p className="text-muted-foreground text-sm">
          You own the entities you create here. Integrations never change them.
        </p>
      </header>
      <EntityForm
        blueprint={blueprint}
        onSaved={(entity) =>
          navigate({
            to: "/catalog/$blueprint/$identifier",
            params: { blueprint: params.blueprint, identifier: entity.identifier },
          })
        }
        onCancel={() => navigate({ to: "/catalog/$blueprint", params })}
      />
    </>
  );
}
