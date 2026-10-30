import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { Property } from "@/features/blueprints";

// PropertyLabel names a property, with its description on hover when it has one.
export function PropertyLabel({ property }: Readonly<{ property: Property }>) {
  if (!property.description) return property.title;
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className="cursor-help text-left underline decoration-dotted underline-offset-4"
          >
            {property.title}
          </button>
        </TooltipTrigger>
        <TooltipContent className="max-w-72">{property.description}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
