import { StateCard } from "@/components/common/state-card";
import type { Blueprint } from "@/features/catalog";
import { BlueprintDetail } from "./blueprint-detail";
import { BlueprintTile } from "./icons";
import { MasterDetail, SidebarItem } from "./master-detail";

export function DataModelTab({
  blueprints,
  selected,
  onSelect,
  onShowIntegration,
}: Readonly<{
  blueprints: Blueprint[];
  selected: string | undefined;
  onSelect: (identifier: string) => void;
  onShowIntegration: (identifier: string) => void;
}>) {
  const current =
    blueprints.find((blueprint) => blueprint.identifier === selected) ?? blueprints[0];

  if (!current) {
    return (
      <StateCard
        title="No blueprints"
        message="Define a blueprint in the definitions tab to shape the catalog."
      />
    );
  }

  return (
    <MasterDetail
      label="Blueprints"
      sidebar={blueprints.map((blueprint) => (
        <SidebarItem
          key={blueprint.identifier}
          selected={blueprint.identifier === current.identifier}
          onSelect={() => onSelect(blueprint.identifier)}
          primary={
            <>
              <BlueprintTile icon={blueprint.icon} />
              <span className="truncate">{blueprint.title}</span>
            </>
          }
          secondary={blueprint.entity_count.toLocaleString("en")}
        />
      ))}
    >
      <BlueprintDetail
        blueprint={current}
        blueprints={blueprints}
        onSelect={onSelect}
        onShowIntegration={onShowIntegration}
      />
    </MasterDetail>
  );
}
