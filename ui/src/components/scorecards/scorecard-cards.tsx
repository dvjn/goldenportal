import { cn } from "cn";
import { Loader2 } from "lucide-react";
import type { ReactNode } from "react";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { EmptyValue } from "@/components/catalog/property-value";
import { Detail, DetailList } from "@/components/detail-list";
import { RunStatusMark, type StatusLook } from "@/components/run-status-mark";
import { checkStatuses } from "@/components/scorecards/check-status";
import { CriterionDetails, FilterSummary } from "@/components/scorecards/criterion-details";
import { TierBadge } from "@/components/scorecards/tier-badge";
import { Card, CardContent } from "@/components/ui/card";
import type { ScorecardSpec, Status, Summary } from "@/features/scorecards";
import { formatAbsolute, formatPercent, formatRelative } from "@/lib/format";

// evaluating looks as a running run does, for results catching up with a publish.
const evaluating: StatusLook = {
  label: "Evaluating",
  icon: Loader2,
  className: "animate-spin text-primary",
};

// AboutCard says what a scorecard grades and when it last did. children
// are details that go first, and action sits in the title bar.
export function AboutCard({
  spec,
  summary,
  action,
  children,
}: Readonly<{ spec: ScorecardSpec; summary?: Summary; action?: ReactNode; children?: ReactNode }>) {
  return (
    <Card>
      <CardTitleBar title="About" action={action} />
      <CardContent className="grid gap-3 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <DetailList>
          {children}
          {summary && (
            <Detail label="Entities">
              <span className="font-medium tabular-nums">{summary.entities}</span>
            </Detail>
          )}
          {summary?.evaluating ? (
            <Detail label="Evaluated">
              <RunStatusMark look={evaluating} label />
            </Detail>
          ) : (
            summary?.evaluatedAt && (
              <Detail label="Evaluated">
                <span title={formatAbsolute(summary.evaluatedAt)}>
                  {formatRelative(summary.evaluatedAt)}
                </span>
              </Detail>
            )
          )}
          {spec.filter && (
            <Detail label="Only">
              <FilterSummary blueprint={spec.blueprint} filter={spec.filter} />
            </Detail>
          )}
        </DetailList>
      </CardContent>
    </Card>
  );
}

// CriteriaResults are how each criterion fares, for CriteriaCard to show
// beside it. onPick, when given, filters to the entities with a status on
// a criterion.
type CriteriaResults = {
  summary?: Summary;
  onPick?: (criterion: string, status: Status) => void;
};

// CriteriaCard lists a scorecard's criteria tier by tier, each with its
// check and fix, and with results, its pass rate and how many entities
// pass, fail and skip it.
export function CriteriaCard({
  spec,
  results,
}: Readonly<{ spec: ScorecardSpec; results?: CriteriaResults }>) {
  const counts = new Map(results?.summary?.criteria.map((c) => [c.identifier, c]));

  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Criteria" />
      <CardContent className="grid px-0">
        {spec.tiers.map((tier) => (
          <CardSection key={tier.name} heading={<TierBadge tiers={spec.tiers} name={tier.name} />}>
            <ul className="grid">
              {tier.criteria.map((criterion) => {
                const c = counts.get(criterion.identifier);
                const applicable = c ? c.passed + c.failed : 0;
                const share = applicable > 0 && c ? (c.passed / applicable) * 100 : null;
                return (
                  <li
                    key={criterion.identifier}
                    className={
                      results
                        ? "grid items-center gap-x-4 gap-y-1 py-1.5 text-sm md:grid-cols-[minmax(0,1fr)_8rem_3rem_auto]"
                        : "grid py-1.5 text-sm"
                    }
                  >
                    <div className="grid min-w-0">
                      <CriterionDetails
                        criterion={criterion}
                        blueprint={spec.blueprint}
                        className="-mb-1 pb-1 font-medium"
                      />
                      {criterion.fix && (
                        <span className="truncate text-xs text-muted-foreground">
                          {criterion.fix}
                        </span>
                      )}
                    </div>
                    {results && (
                      <>
                        <div className="h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
                          <div
                            className="h-full rounded-full bg-emerald-500"
                            style={{ width: `${share ?? 0}%` }}
                          />
                        </div>
                        <span className="text-xs text-muted-foreground tabular-nums md:text-right">
                          {c && share !== null ? (
                            formatPercent(c.passed, applicable)
                          ) : (
                            <EmptyValue title="No entity has been checked" />
                          )}
                        </span>
                        <StatusCounts
                          title={criterion.title}
                          counts={c}
                          onPick={
                            results.onPick &&
                            ((status) => results.onPick?.(criterion.identifier, status))
                          }
                        />
                      </>
                    )}
                  </li>
                );
              })}
            </ul>
          </CardSection>
        ))}
      </CardContent>
    </Card>
  );
}

const countedStatuses: Status[] = ["passed", "failed", "skipped"];

// StatusCounts says how many entities have each status on a criterion. With
// onPick, every count picks its status, none too, so a filter on one status
// can move to another.
function StatusCounts({
  title,
  counts,
  onPick,
}: Readonly<{
  title: string;
  counts?: Record<Status, number>;
  onPick?: (status: Status) => void;
}>) {
  return (
    <span className="flex gap-3 text-xs text-muted-foreground tabular-nums">
      {countedStatuses.map((status) => {
        const n = counts?.[status] ?? 0;
        const text = `${n} ${checkStatuses[status].label.toLowerCase()}`;
        const className = cn("w-20 text-left", n > 0 && checkStatuses[status].className);
        if (!onPick) {
          return (
            <span key={status} className={className}>
              {text}
            </span>
          );
        }
        return (
          <button
            key={status}
            type="button"
            className={cn(className, "hover:underline")}
            aria-label={`${title}: ${text}`}
            onClick={() => onPick(status)}
          >
            {text}
          </button>
        );
      })}
    </span>
  );
}
