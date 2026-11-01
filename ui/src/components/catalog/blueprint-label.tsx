import { useQuery } from "@tanstack/react-query";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { catalogQueryOptions } from "@/features/catalog";

// BlueprintLabel names a blueprint by its icon and title, or by its
// identifier while it isn't in the catalog, such as before it's published.
export function BlueprintLabel({ blueprint }: Readonly<{ blueprint: string }>) {
  const { data: catalog } = useQuery(catalogQueryOptions);
  const entry = catalog?.find((e) => e.identifier === blueprint);
  return (
    <span className="flex min-w-0 items-center gap-2">
      <IconTile icon={entry?.icon} color={entry?.color} size="sm" />
      <span className="truncate">{entry?.title ?? blueprint}</span>
    </span>
  );
}
