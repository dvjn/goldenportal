import { useQuery } from "@tanstack/react-query";
import { EntityLink } from "@/components/catalog/entity-link";
import { EmptyValue } from "@/components/catalog/property-value";
import { scorecards, type Tier } from "@/features/scorecards";
import { TierBadge } from "./tier-badge";

// ScoreLink shows the tier an entity reached, linked to its result on the
// scorecard, as a relation links to its entity. A null tier means the
// scorecard hasn't scored the entity, which empty, if given, says instead.
export function ScoreLink({
  scorecard,
  tiers,
  tier,
  entity,
  empty,
}: Readonly<{
  scorecard: string;
  tiers: Tier[];
  tier: string | null;
  entity?: { blueprint: string; identifier: string };
  empty?: string;
}>) {
  if (tier === null) return <EmptyValue label={empty} title="Not evaluated" />;
  const badge = <TierBadge tiers={tiers} name={tier} />;
  if (!entity) return badge;
  return (
    <EntityLink
      blueprint={entity.blueprint}
      identifier={entity.identifier}
      tab={scorecard}
      className="hover:underline"
    >
      {badge}
    </EntityLink>
  );
}

// ScorecardValue shows a scorecard property's value, with the tiers its
// scorecard names.
export function ScorecardValue({
  scorecard,
  value,
  entity,
  empty,
}: Readonly<{
  scorecard: string;
  value: unknown;
  entity?: { blueprint: string; identifier: string };
  empty?: string;
}>) {
  const { data: doc } = useQuery(scorecards.queries.detail(scorecard));
  const tier =
    typeof value === "object" && value !== null && "tier" in value && typeof value.tier === "string"
      ? value.tier
      : null;
  return (
    <ScoreLink
      scorecard={scorecard}
      tiers={doc?.published?.spec.tiers ?? []}
      tier={tier}
      entity={entity}
      empty={empty}
    />
  );
}
