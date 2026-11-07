import { EmptyValue } from "@/components/catalog/property-value";
import { formatPercent } from "@/lib/format";

// ChecksPassing leads a scorecard's results with the share of checks that
// pass. Errored checks count as failing.
export function ChecksPassing({ passed, failed }: Readonly<{ passed: number; failed: number }>) {
  return (
    <div className="flex items-baseline gap-2">
      <span className="text-2xl font-semibold tabular-nums">
        {passed + failed === 0 ? (
          <EmptyValue title="No checks have run" />
        ) : (
          formatPercent(passed, passed + failed)
        )}
      </span>
      <span className="text-sm text-muted-foreground">checks passing</span>
    </div>
  );
}

// checkTotals sums a scorecard summary's criteria into passed and failed.
export function checkTotals(criteria: { passed: number; failed: number; errored: number }[]): {
  passed: number;
  failed: number;
} {
  return criteria.reduce(
    (sum, c) => ({ passed: sum.passed + c.passed, failed: sum.failed + c.failed + c.errored }),
    { passed: 0, failed: 0 },
  );
}
