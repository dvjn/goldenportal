import { Skeleton } from "@/components/ui/skeleton";

const rowIds = ["row-1", "row-2", "row-3", "row-4", "row-5"];

export function ListSkeleton() {
  return (
    <div className="space-y-3 rounded-xl border bg-card p-5 shadow-xs">
      <Skeleton className="h-8 w-40" />
      {rowIds.map((id) => (
        <Skeleton key={id} className="h-12 w-full" />
      ))}
    </div>
  );
}
