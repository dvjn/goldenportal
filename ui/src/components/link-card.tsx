import { createLink } from "@tanstack/react-router";
import { cn } from "cn";
import type { ComponentPropsWithRef, ReactNode } from "react";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

// A card that's a link is its title's link stretched across the card, so
// the card's content stays plain markup and the link's name is the title.
// The card hovers like every other interactive surface, and the stretched
// link draws the focus ring around the whole card.
export const interactiveCard = "relative transition-colors hover:bg-accent";
export const stretchedLink =
  "after:absolute after:inset-0 after:rounded-xl focus-visible:outline-none focus-visible:after:ring-3 focus-visible:after:ring-ring/50";

type CardWithLinkProps = Omit<ComponentPropsWithRef<"a">, "title" | "className"> & {
  icon?: string;
  color?: string;
  title: string;
  description?: string;
  className?: string;
  children?: ReactNode;
};

// CardWithLink is the card itself; the router gives it the anchor's props
// through createLink, so LinkCard is typed like Link.
function CardWithLink({
  icon,
  color,
  title,
  description,
  className,
  children,
  ...anchor
}: CardWithLinkProps) {
  return (
    <Card className={cn("h-full", interactiveCard, className)}>
      <CardHeader>
        <div className="flex items-start gap-3">
          <IconTile icon={icon} color={color} />
          <div className="grid min-w-0 gap-1">
            <CardTitle className="truncate">
              <a {...anchor} className={stretchedLink}>
                {title}
              </a>
            </CardTitle>
            {description && (
              <CardDescription className="line-clamp-2">{description}</CardDescription>
            )}
          </div>
        </div>
      </CardHeader>
      {children}
    </Card>
  );
}

// LinkCard opens somewhere from a grid of cards: its icon, title and
// description, with anything more below.
export const LinkCard = createLink(CardWithLink);
