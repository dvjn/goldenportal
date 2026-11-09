import { formatNumber } from "@/components/catalog/property-value";
import type { Reading, Summary } from "@/features/widgets";
import { formatPercent } from "@/lib/format";

const ops = ["sum", "avg", "min", "max", "p50", "p90", "p95", "p99", "share"] as const;

// summaryOp is how a summary sums up its entities: count, unless it names
// another way.
export function summaryOp(summary: Summary | undefined) {
  return ops.find((op) => summary?.[op] !== undefined) ?? "count";
}

// isShare reports whether a reading is a share, which says how many of how
// many it is.
export function isShare(reading: Reading): reading is Reading & { count: number; total: number } {
  return reading.count !== undefined && reading.total !== undefined;
}

// formatReading writes a reading's value in its unit; a share works its
// percent out from its counts, as stats do, so it rounds the same way.
export function formatReading(reading: Reading, unit?: string) {
  if (reading.value === null) return undefined;
  if (isShare(reading) && reading.total > 0) return formatPercent(reading.count, reading.total);
  return formatNumber(reading.value, unit);
}
