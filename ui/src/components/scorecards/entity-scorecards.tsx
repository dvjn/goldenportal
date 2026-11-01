import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { cn } from "cn";
import { Check, ChevronDown, CircleCheck, CircleMinus, CircleX, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { CardTitleBar } from "@/components/card-sections";
import { CriterionDetails } from "@/components/scorecards/criterion-details";
import { TierBadge, tierColors } from "@/components/scorecards/tier-badge";
import { Card, CardContent } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  type Criterion,
  type EntityScorecard,
  type ScorecardSpec,
  type Status,
  scorecards,
  type Tier,
} from "@/features/scorecards";
import { formatAbsolute } from "@/lib/format";

const statuses: Record<Status, { icon: typeof CircleCheck; className: string; label: string }> = {
  passed: {
    icon: CircleCheck,
    className: "text-emerald-600 dark:text-emerald-400",
    label: "Passed",
  },
  failed: { icon: CircleX, className: "text-destructive", label: "Failed" },
  skipped: { icon: CircleMinus, className: "text-muted-foreground", label: "Skipped" },
  errored: {
    icon: TriangleAlert,
    className: "text-amber-600 dark:text-amber-400",
    label: "Errored",
  },
};

const blocking = (status?: Status) => status === "failed" || status === "errored";

// ScorecardTab labels an entity's tab for one scorecard with the tier reached.
export function ScorecardTab({ result }: Readonly<{ result: EntityScorecard }>) {
  const { data: doc } = useQuery(scorecards.queries.detail(result.scorecard));
  const tiers = doc?.published?.spec.tiers ?? [];
  return (
    <span className="flex items-center gap-1.5">
      {result.scorecardTitle}
      <span className={`size-2 rounded-full ${tierColors(tiers, result.tier).dot}`} aria-hidden />
    </span>
  );
}

// ScorecardResult shows how an entity does on one scorecard: where it is on
// the ladder, and every criterion, tier by tier.
export function ScorecardResult({ result }: Readonly<{ result: EntityScorecard }>) {
  const { data: doc } = useQuery(scorecards.queries.detail(result.scorecard));
  const spec = doc?.published?.spec;
  if (!spec) return null;

  const reached = spec.tiers.findIndex((tier) => tier.name === result.tier);

  return (
    <div className="grid gap-4">
      <StandingCard spec={spec} result={result} reached={reached} />
      <CriteriaCard spec={spec} result={result} />
    </div>
  );
}

// StandingCard shows where the entity stands: its tier, its score, and a
// mark for every criterion.
function StandingCard({
  spec,
  result,
  reached,
}: Readonly<{ spec: ScorecardSpec; result: EntityScorecard; reached: number }>) {
  // Marks keep the scorecard's order, tier by tier, so each check is always
  // in the same place.
  const criteria = spec.tiers.flatMap((tier) => tier.criteria);

  return (
    <Card>
      <CardContent className="grid gap-5">
        <div className="flex items-start justify-between gap-6">
          <div className="grid gap-1">
            <Link
              to="/scorecards/$identifier"
              params={{ identifier: result.scorecard }}
              className="w-fit text-xs font-medium hover:underline"
              title={`Evaluated ${formatAbsolute(result.evaluatedAt)}`}
            >
              {spec.title}
            </Link>
            <TierBadge
              tiers={spec.tiers}
              name={result.tier}
              className="text-2xl leading-8 [&>span]:size-3"
            />
          </div>
          <div className="grid justify-items-end gap-1.5">
            <span className="text-2xl leading-8 font-semibold tabular-nums">
              {result.score === null ? "-" : `${result.score}%`}
            </span>
            <Marks criteria={criteria} result={result} label="Criteria" />
          </div>
        </div>
        <Ladder tiers={spec.tiers} reached={reached} />
      </CardContent>
    </Card>
  );
}

// Marks shows each criterion's outcome as its icon, named on hover.
function Marks({
  criteria,
  result,
  label,
}: Readonly<{ criteria: Criterion[]; result: EntityScorecard; label: string }>) {
  return (
    <ul className="flex flex-wrap justify-end gap-0.5" aria-label={label}>
      {criteria.map((criterion) => {
        const status = result.criteria[criterion.identifier]?.status ?? "skipped";
        const { icon: Icon, className, label } = statuses[status];
        return (
          <li key={criterion.identifier} title={`${criterion.title}: ${label}`}>
            <Icon className={cn("size-4", className)} aria-label={`${criterion.title}: ${label}`} />
          </li>
        );
      })}
    </ul>
  );
}

// Ladder is one bar split into the tiers, lowest first: reached tiers fill
// with their color, the next one is outlined, the rest stay empty.
function Ladder({ tiers, reached }: Readonly<{ tiers: Tier[]; reached: number }>) {
  return (
    <ol
      className="grid gap-1.5"
      style={{ gridTemplateColumns: `repeat(${tiers.length}, minmax(0, 1fr))` }}
      aria-label="Tiers"
    >
      {tiers.map((tier, index) => {
        const color = tierColors(tiers, tier.name);
        const isReached = index <= reached;
        const isNext = index === reached + 1;
        return (
          <li key={tier.name} className="grid gap-1.5">
            <span
              className={cn(
                "h-2 rounded-full",
                isReached ? color.dot : "bg-muted",
                isNext && "ring-1 ring-foreground/30 ring-inset",
              )}
              aria-hidden
            />
            <span
              className={cn(
                "flex items-center gap-1 text-xs",
                isReached ? cn("font-medium", color.text) : "text-muted-foreground",
              )}
            >
              {isReached && <Check className="size-3" strokeWidth={3} aria-hidden />}
              {tier.title}
              <span className="sr-only">
                {isReached ? " (reached)" : isNext ? " (next)" : " (ahead)"}
              </span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function CriteriaCard({
  spec,
  result,
}: Readonly<{ spec: ScorecardSpec; result: EntityScorecard }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Criteria" />
      <CardContent className="grid px-0">
        {spec.tiers.map((tier) => (
          <TierSection key={tier.name} spec={spec} tier={tier} result={result} />
        ))}
      </CardContent>
    </Card>
  );
}

// TierSection is a tier's row, marking its criteria, that opens to list
// them in the scorecard's order.
function TierSection({
  spec,
  tier,
  result,
}: Readonly<{
  spec: ScorecardSpec;
  tier: Tier;
  result: EntityScorecard;
}>) {
  const failing = tier.criteria.some((c) => blocking(result.criteria[c.identifier]?.status));
  // A tier with something to fix starts open; the rest fold away.
  const [open, setOpen] = useState(failing);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="border-b last:border-b-0">
      <CollapsibleTrigger className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition-colors outline-none hover:bg-accent focus-visible:bg-accent">
        <span className="flex items-center gap-2">
          <ChevronDown
            className={cn(
              "size-4 text-muted-foreground transition-transform",
              !open && "-rotate-90",
            )}
            aria-hidden
          />
          <TierBadge tiers={spec.tiers} name={tier.name} />
        </span>
        <span className="flex items-center gap-2">
          <Marks criteria={tier.criteria} result={result} label={`${tier.title} criteria`} />
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <ul className="grid gap-2 px-4 pb-3">
          {tier.criteria.map((criterion) => (
            <CriterionRow key={criterion.identifier} criterion={criterion} result={result} />
          ))}
        </ul>
      </CollapsibleContent>
    </Collapsible>
  );
}

function CriterionRow({
  criterion,
  result,
}: Readonly<{ criterion: Criterion; result: EntityScorecard }>) {
  const outcome = result.criteria[criterion.identifier] ?? { status: "skipped" as Status };
  const { icon: Icon, className, label } = statuses[outcome.status];

  return (
    <li className="flex items-start gap-2.5 text-sm">
      <Icon className={cn("mt-0.5 size-4 shrink-0", className)} aria-label={label} />
      <div className="grid min-w-0 flex-1 gap-0.5">
        <CriterionDetails
          criterion={criterion}
          className={cn(outcome.status === "skipped" && "text-muted-foreground")}
        />
        {outcome.status === "errored" && outcome.message && (
          <span className="font-mono text-xs text-amber-700 dark:text-amber-400">
            {outcome.message}
          </span>
        )}
        {outcome.status === "skipped" && (
          <span className="text-xs text-muted-foreground">Doesn't apply to this entity</span>
        )}
        {criterion.fix && <span className="text-xs text-muted-foreground">{criterion.fix}</span>}
      </div>
    </li>
  );
}
