import { useQuery, useQueryClient } from "@tanstack/react-query";
import { type BlueprintSpec, blueprints, type Property } from "@/features/blueprints";

// useLookupTarget finds the property a lookup reads, by following its path
// through the blueprints its relations lead to, so the value shows the way
// it does on its own entity.
export function useLookupTarget(blueprint: string | undefined, lookup: string | undefined) {
  const client = useQueryClient();
  return useQuery({
    queryKey: ["catalog", "lookup-target", blueprint, lookup],
    enabled: Boolean(blueprint && lookup),
    staleTime: 60_000,
    queryFn: async (): Promise<Property | null> => {
      let current = blueprint;
      const segments = lookup?.split(".") ?? [];
      for (const [i, segment] of segments.entries()) {
        if (!current) return null;
        const spec: BlueprintSpec | undefined = (
          await client.ensureQueryData(blueprints.queries.detail(current))
        ).published?.spec;
        const property = spec?.properties.find((p) => p.identifier === segment);
        if (!property) return null;
        if (i === segments.length - 1) return property;
        current = property.relation;
      }
      return null;
    },
  }).data;
}
