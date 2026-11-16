import { cn } from "cn";
import { Loader2 } from "lucide-react";
import type { ReactNode } from "react";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { checkPath } from "@/components/catalog/catalog-filters";
import { Detail, DetailList } from "@/components/detail-list";
import { RunStatusMark, type StatusLook } from "@/components/run-status-mark";
import { checkStatuses } from "@/components/scorecards/check-status";
import { CountLink, type ResultsPath } from "@/components/scorecards/count-link";
import { CriterionDetails, FilterSummary } from "@/components/scorecards/criterion-details";
import { TierBadge } from "@/components/scorecards/tier-badge";
import { Card, CardContent } from "@/components/ui/card";
import type { ScorecardSpec, Status, Summary } from "@/features/scorecards";
import { formatAbsolute, formatCount, formatRelative } from "@/lib/format";

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
              <span className="font-medium tabular-nums">{formatCount(summary.entities)}</span>
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

// CriteriaResults are how each criterion fares, for CriteriaCard to show beside it.
type CriteriaResults = {
  summary?: Summary;
  link?: ResultsPath;
};

// CriteriaCard lists a scorecard's criteria tier by tier, each with its
// check and fix, and with results, its pass rate and how many entities
// pass, fail and skip it. notice says why results that were asked for
// aren't there.
export function CriteriaCard({
  spec,
  results,
  notice,
}: Readonly<{ spec: ScorecardSpec; results?: CriteriaResults; notice?: string }>) {
  const counts = new Map(results?.summary?.criteria.map((c) => [c.identifier, c]));

  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Criteria" />
      <CardContent className="grid px-0">
        {notice && <p className="px-(--card-spacing) text-sm text-muted-foreground">{notice}</p>}
        {spec.tiers.map((tier) => (
          <CardSection key={tier.name} heading={<TierBadge tiers={spec.tiers} name={tier.name} />}>
            <ul className="grid">
              {tier.criteria.map((criterion) => {
                const c = counts.get(criterion.identifier);
                const total = c ? c.passed + c.failed + c.skipped : 0;
                return (
                  <li
                    key={criterion.identifier}
                    className={
                      results
                        ? "grid items-center gap-x-4 gap-y-1 py-1.5 text-sm md:grid-cols-[minmax(0,1fr)_8rem_auto]"
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
                        <div
                          className="flex h-1.5 overflow-hidden rounded-full bg-muted"
                          aria-hidden
                        >
                          {c &&
                            total > 0 &&
                            countedStatuses.map((status) => (
                              <div
                                key={status}
                                className={barColors[status]}
                                style={{ width: `${(c[status] / total) * 100}%` }}
                              />
                            ))}
                        </div>
                        <StatusCounts
                          title={criterion.title}
                          counts={c}
                          link={
                            results.link && {
                              ...results.link,
                              path: checkPath(results.link.path, criterion.identifier),
                            }
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

const barColors: Record<Status, string> = {
  passed: "bg-emerald-500",
  failed: "bg-destructive",
  skipped: "bg-muted-foreground/40",
};

// StatusCounts says how many entities have each status on a criterion.
function StatusCounts({
  title,
  counts,
  link,
}: Readonly<{
  title: string;
  counts?: Record<Status, number>;
  link?: ResultsPath;
}>) {
  return (
    <span className="flex gap-3 text-xs text-muted-foreground tabular-nums">
      {countedStatuses.map((status) => {
        const n = counts?.[status] ?? 0;
        const { icon: Icon, className, label } = checkStatuses[status];
        const text = `${formatCount(n)} ${label.toLowerCase()}`;
        return (
          <CountLink
            key={status}
            link={link}
            value={status}
            count={n}
            label={`${title}: ${text}`}
            className={cn("w-12", n > 0 && className)}
          >
            <span title={text} className="inline-flex items-center gap-1">
              {formatCount(n)}
              <Icon className="size-3.5" aria-hidden />
              <span className="sr-only">{` ${label.toLowerCase()}`}</span>
            </span>
          </CountLink>
        );
      })}
    </span>
  );
}
