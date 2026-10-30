import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

// PagePending holds a page's place while its data loads.
export function PagePending() {
  return (
    <div className="flex flex-col gap-6" aria-busy>
      <div className="grid gap-2">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </div>
      <Card>
        <CardContent className="grid gap-3">
          {[0, 1, 2, 3, 4].map((row) => (
            <Skeleton key={row} className="h-4 w-full" />
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
