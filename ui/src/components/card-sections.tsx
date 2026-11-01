import { cn } from "cn";
import type { ReactNode } from "react";
import { CardAction, CardHeader, CardTitle } from "@/components/ui/card";

// Cards have two kinds of heading. A card header titles the whole card, or
// table, as a bar across its top. A section header names one group of rows
// inside a card, and sections are set apart by rules. Nothing is headed
// from outside its card: the page or tab names what fills it.

// CardTitleBar is a card's header: its title, with any action on the right.
export function CardTitleBar({
  title,
  action,
}: Readonly<{ title: ReactNode; action?: ReactNode }>) {
  return (
    <CardHeader className="border-b">
      <CardTitle>{title}</CardTitle>
      {action && <CardAction className="flex items-center gap-2 self-center">{action}</CardAction>}
    </CardHeader>
  );
}

// CardSection is one group of rows in a card. title labels it in words;
// heading names it by a thing instead, such as a tier or a blueprint.
// Sections go in a CardContent without padding, so their rules run edge
// to edge.
export function CardSection({
  title,
  heading,
  action,
  className,
  children,
}: Readonly<{
  title?: string;
  heading?: ReactNode;
  action?: ReactNode;
  className?: string;
  children?: ReactNode;
}>) {
  return (
    <section className={cn("grid gap-2 border-b px-4 py-3 last:border-b-0", className)}>
      <div className="flex min-w-0 items-center justify-between gap-3">
        {title ? (
          <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            {title}
          </h3>
        ) : (
          <h3 className="flex min-w-0 items-center gap-2 text-sm font-medium">{heading}</h3>
        )}
        {action}
      </div>
      {children}
    </section>
  );
}
