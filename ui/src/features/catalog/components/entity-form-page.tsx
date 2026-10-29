import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useBreadcrumbAncestors } from "@/components/common/breadcrumb-trail";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { StateCard } from "@/components/common/state-card";
import { usePageTitle } from "@/lib/use-page-title";
import type { Blueprint, Entity } from "../api";
import { EntityForm } from "../entity-form";
import { blueprintQueryOptions, entityQueryOptions } from "../queries";
import { BlueprintIconTile } from "./blueprint-icon";

const listPath = (blueprint: string) => `/catalog/${encodeURIComponent(blueprint)}`;

const entityPath = (blueprint: string, identifier: string) =>
  `${listPath(blueprint)}/${encodeURIComponent(identifier)}`;

function Unavailable() {
  return <StateCard title="Form unavailable" message="We could not load this blueprint." />;
}

function NewEntity({ blueprint }: Readonly<{ blueprint: Blueprint }>) {
  const navigate = useNavigate();
  const title = `New ${blueprint.title.toLowerCase()}`;
  useBreadcrumbAncestors([{ label: blueprint.title, to: listPath(blueprint.identifier) }]);
  usePageTitle(title);

  return (
    <>
      <PageHeader
        title={title}
        description="You own the entities you create here. Integrations never change them."
        icon={<BlueprintIconTile icon={blueprint.icon} />}
      />
      <EntityForm
        blueprint={blueprint}
        onSaved={(entity) =>
          navigate({
            to: "/catalog/$blueprint/$identifier",
            params: { blueprint: blueprint.identifier, identifier: entity.identifier },
          })
        }
        onCancel={() =>
          navigate({ to: "/catalog/$blueprint", params: { blueprint: blueprint.identifier } })
        }
      />
    </>
  );
}

export function NewEntityPage({ blueprintId }: Readonly<{ blueprintId: string }>) {
  const blueprint = useQuery(blueprintQueryOptions(blueprintId));
  if (blueprint.isError) return <Unavailable />;
  if (!blueprint.data) return <ListSkeleton />;
  return <NewEntity blueprint={blueprint.data} />;
}

function EditEntity({ blueprint, entity }: Readonly<{ blueprint: Blueprint; entity: Entity }>) {
  const navigate = useNavigate();
  const back = () =>
    navigate({
      to: "/catalog/$blueprint/$identifier",
      params: { blueprint: blueprint.identifier, identifier: entity.identifier },
    });
  const title = `Edit ${entity.title || entity.identifier}`;
  useBreadcrumbAncestors([
    { label: blueprint.title, to: listPath(blueprint.identifier) },
    { label: entity.title, to: entityPath(blueprint.identifier, entity.identifier) },
  ]);
  usePageTitle(title);

  return (
    <>
      <PageHeader
        title={title}
        description={<span className="font-mono">{entity.identifier}</span>}
        icon={<BlueprintIconTile icon={blueprint.icon} />}
      />
      <EntityForm blueprint={blueprint} entity={entity} onSaved={back} onCancel={back} />
    </>
  );
}

export function EditEntityPage({
  blueprintId,
  identifier,
}: Readonly<{ blueprintId: string; identifier: string }>) {
  const blueprint = useQuery(blueprintQueryOptions(blueprintId));
  const entity = useQuery(entityQueryOptions(blueprintId, identifier));
  if (blueprint.isError || entity.isError) return <Unavailable />;
  if (!blueprint.data || !entity.data) return <ListSkeleton />;
  return <EditEntity blueprint={blueprint.data} entity={entity.data} />;
}
