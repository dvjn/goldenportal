import { cn } from "cn";
import { CardSection } from "@/components/card-sections";
import { CodeBlock } from "@/components/code";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import type { Criterion } from "@/features/scorecards";

// CriterionDetails names a criterion and shows what it checks on hover.
export function CriterionDetails({
  criterion,
  className,
}: Readonly<{ criterion: Criterion; className?: string }>) {
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
          <CodeBlock>{criterion.check}</CodeBlock>
        </CardSection>
        {criterion.when && (
          <CardSection title="Only when">
            <CodeBlock>{criterion.when}</CodeBlock>
          </CardSection>
        )}
      </HoverCardContent>
    </HoverCard>
  );
}
