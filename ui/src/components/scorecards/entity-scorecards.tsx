import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { cn } from "cn";
import {
  Check,
  ChevronDown,
  CircleCheck,
  CircleMinus,
  CircleX,
  TriangleAlert,
  Trophy,
} from "lucide-react";
import { useState } from "react";
import { TierBadge, tierColors } from "@/components/scorecards/tier-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import {
  type Criterion,
  type EntityScorecard,
  type ScorecardSpec,
  type Status,
  scorecards,
  type Tier,
} from "@/features/scorecards";
import { formatAbsolute, formatRelative } from "@/lib/format";

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
// the ladder, what stands between it and the next tier, and every criterion.
export function ScorecardResult({ result }: Readonly<{ result: EntityScorecard }>) {
  const { data: doc } = useQuery(scorecards.queries.detail(result.scorecard));
  const spec = doc?.published?.spec;
  if (!spec) return null;

  const reached = spec.tiers.findIndex((tier) => tier.name === result.tier);
  const next = spec.tiers[reached + 1];

  return (
    <div className="grid gap-4">
      <StandingCard spec={spec} result={result} reached={reached} next={next} />
      <CriteriaCard spec={spec} result={result} reached={reached} />
    </div>
  );
}

// StandingCard shows where the entity stands, and what it needs next.
function StandingCard({
  spec,
  result,
  reached,
  next,
}: Readonly<{ spec: ScorecardSpec; result: EntityScorecard; reached: number; next?: Tier }>) {
  const applicable = result.passed + result.failed + result.errored;

  return (
    <Card>
      <CardContent className="grid gap-5">
        <div className="flex items-start justify-between gap-6">
          <div className="grid gap-1">
            <span className="text-xs text-muted-foreground">
              <Link
                to="/scorecards/$identifier"
                params={{ identifier: result.scorecard }}
                className="font-medium text-foreground hover:underline"
              >
                {spec.title}
              </Link>
              {" · "}
              <span title={formatAbsolute(result.evaluatedAt)}>
                evaluated {formatRelative(result.evaluatedAt)}
              </span>
            </span>
            <TierBadge
              tiers={spec.tiers}
              name={result.tier}
              className="text-2xl leading-8 [&>span]:size-3"
            />
          </div>
          <div className="grid justify-items-end gap-1">
            <span className="text-xs text-muted-foreground">
              {result.passed} of {applicable} criteria passing
              {result.skipped > 0 && `, ${result.skipped} skipped`}
              {result.errored > 0 && `, ${result.errored} errored`}
            </span>
            <span className="text-2xl leading-8 font-semibold tabular-nums">
              {result.score === null ? "—" : `${result.score}%`}
            </span>
          </div>
        </div>
        <Ladder tiers={spec.tiers} reached={reached} />
        <NextTier spec={spec} result={result} reached={reached} next={next} />
      </CardContent>
    </Card>
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
                isReached ? color.bar : "bg-muted",
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
              {isNext && <span className="text-muted-foreground"> · next</span>}
              <span className="sr-only">{isReached ? " (reached)" : isNext ? "" : " (ahead)"}</span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function NextTier({
  spec,
  result,
  reached,
  next,
}: Readonly<{ spec: ScorecardSpec; result: EntityScorecard; reached: number; next?: Tier }>) {
  if (!next) {
    return (
      <p className="flex items-center gap-2 border-t pt-4 text-sm">
        <Trophy className="size-4 text-amber-500" aria-hidden />
        Reached {spec.tiers.at(-1)?.title}, the top tier.
      </p>
    );
  }

  // A tier needs every tier below it, so failures below the next one count too.
  const needed = spec.tiers
    .slice(0, reached + 2)
    .flatMap((tier) => tier.criteria)
    .filter((criterion) => blocking(result.criteria[criterion.identifier]?.status));

  return (
    <section className="grid gap-3 border-t pt-4">
      <h3 className="flex items-center justify-between gap-3 text-sm font-medium">
        <span className="flex items-center gap-1.5">
          To reach <TierBadge tiers={spec.tiers} name={next.name} />
        </span>
        <span className="text-xs font-normal text-muted-foreground">
          {needed.length} {needed.length === 1 ? "criterion" : "criteria"} left
        </span>
      </h3>
      <ul className="grid gap-3">
        {needed.map((criterion) => (
          <CriterionRow key={criterion.identifier} criterion={criterion} result={result} showFix />
        ))}
      </ul>
    </section>
  );
}

function CriteriaCard({
  spec,
  result,
  reached,
}: Readonly<{ spec: ScorecardSpec; result: EntityScorecard; reached: number }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="border-b">
        <CardTitle>Criteria</CardTitle>
      </CardHeader>
      <CardContent className="grid px-0">
        {spec.tiers.map((tier, index) => (
          <TierSection
            key={tier.name}
            spec={spec}
            tier={tier}
            result={result}
            met={index <= reached}
            // Tiers above the first unmet one wait on it, however they do.
            waitsOn={index > reached + 1 ? spec.tiers[reached + 1] : undefined}
          />
        ))}
      </CardContent>
    </Card>
  );
}

// TierSection lists a tier's criteria, open ones first; passed and skipped
// ones fold away, since they need nothing.
function TierSection({
  spec,
  tier,
  result,
  met,
  waitsOn,
}: Readonly<{
  spec: ScorecardSpec;
  tier: Tier;
  result: EntityScorecard;
  met: boolean;
  waitsOn?: Tier;
}>) {
  const [open, setOpen] = useState(false);
  const status = (c: Criterion) => result.criteria[c.identifier]?.status;
  const outstanding = tier.criteria.filter((c) => blocking(status(c)));
  const settled = tier.criteria.filter((c) => !blocking(status(c)));
  const passing = tier.criteria.filter((c) => status(c) === "passed").length;
  const applicable = tier.criteria.filter((c) => status(c) !== "skipped").length;

  return (
    <section className="grid gap-2 border-b px-4 py-3 last:border-b-0">
      <div className="flex items-center justify-between gap-3">
        <TierBadge tiers={spec.tiers} name={tier.name} />
        {met ? (
          <Badge variant="secondary">Met</Badge>
        ) : (
          <span className="text-xs text-muted-foreground tabular-nums">
            {outstanding.length === 0 ? "All passing" : `${passing} of ${applicable} passing`}
            {waitsOn && ` · needs ${waitsOn.title} first`}
          </span>
        )}
      </div>
      {outstanding.length > 0 && (
        <ul className="grid gap-2">
          {outstanding.map((criterion) => (
            <CriterionRow
              key={criterion.identifier}
              criterion={criterion}
              result={result}
              showFix
            />
          ))}
        </ul>
      )}
      {settled.length > 0 && (
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger asChild>
            <Button variant="ghost" size="sm" className="-ml-2 h-7 text-muted-foreground">
              <ChevronDown className={cn("transition-transform", open && "rotate-180")} />
              {open ? "Hide" : "Show"} {settled.length} passed or skipped
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="grid gap-2 pt-1">
              {settled.map((criterion) => (
                <CriterionRow key={criterion.identifier} criterion={criterion} result={result} />
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      )}
    </section>
  );
}

function CriterionRow({
  criterion,
  result,
  showFix,
}: Readonly<{ criterion: Criterion; result: EntityScorecard; showFix?: boolean }>) {
  const outcome = result.criteria[criterion.identifier] ?? { status: "skipped" as Status };
  const { icon: Icon, className, label } = statuses[outcome.status];

  return (
    <li className="flex items-start gap-2.5 text-sm">
      <Icon className={cn("mt-0.5 size-4 shrink-0", className)} aria-label={label} />
      <div className="grid min-w-0 flex-1 gap-0.5">
        <HoverCard openDelay={200}>
          <HoverCardTrigger asChild>
            <span
              className={cn(
                "w-fit cursor-default underline decoration-muted-foreground/40 decoration-dotted underline-offset-4",
                outcome.status === "skipped" && "text-muted-foreground",
              )}
            >
              {criterion.title}
            </span>
          </HoverCardTrigger>
          <HoverCardContent align="start" className="grid w-md gap-2">
            <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              Check
            </span>
            <pre className="rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-5 whitespace-pre-wrap">
              {criterion.check}
            </pre>
            {criterion.when && (
              <>
                <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                  Only when
                </span>
                <pre className="rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-5 whitespace-pre-wrap">
                  {criterion.when}
                </pre>
              </>
            )}
          </HoverCardContent>
        </HoverCard>
        {outcome.status === "errored" && outcome.message && (
          <span className="font-mono text-xs text-amber-700 dark:text-amber-400">
            {outcome.message}
          </span>
        )}
        {outcome.status === "skipped" && (
          <span className="text-xs text-muted-foreground">Doesn't apply to this entity</span>
        )}
        {showFix && criterion.fix && (
          <span className="text-xs text-muted-foreground">{criterion.fix}</span>
        )}
      </div>
    </li>
  );
}
