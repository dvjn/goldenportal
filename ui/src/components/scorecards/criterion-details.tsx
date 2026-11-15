import { cn } from "cn";
import { CardSection } from "@/components/card-sections";
import { useFixedSummaries } from "@/components/catalog/where-filters";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import type { Filter } from "@/features/catalog";
import type { Criterion } from "@/features/scorecards";

// CriterionDetails names a criterion and shows what it checks on hover.
// blueprint is the one the scorecard grades, which names the filters' fields.
export function CriterionDetails({
  criterion,
  blueprint,
  className,
}: Readonly<{ criterion: Criterion; blueprint: string; className?: string }>) {
  return (
    <HoverCard openDelay={200}>
      <HoverCardTrigger asChild>
        <span
          className={cn(
            "w-fit max-w-full cursor-default truncate underline decoration-muted-foreground/40 decoration-dotted underline-offset-4",
            className,
          )}
        >
          {criterion.title}
        </span>
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-md p-0">
        {criterion.description && (
          <p className="border-b px-4 py-3 text-sm">{criterion.description}</p>
        )}
        <CardSection title="Check">
          <FilterSummary blueprint={blueprint} filter={criterion.check} className="text-sm" />
        </CardSection>
        {criterion.when && (
          <CardSection title="Only When">
            <FilterSummary blueprint={blueprint} filter={criterion.when} className="text-sm" />
          </CardSection>
        )}
      </HoverCardContent>
    </HoverCard>
  );
}

// FilterSummary says a filter on blueprint's entities as the catalog's chips do.
export function FilterSummary({
  blueprint,
  filter,
  className,
}: Readonly<{ blueprint: string; filter: Filter; className?: string }>) {
  const [said] = useFixedSummaries(blueprint, [filter]);
  return (
    <p className={className}>
      {said.title}: {said.summary}
    </p>
  );
}
