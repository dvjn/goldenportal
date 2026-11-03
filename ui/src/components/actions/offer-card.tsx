import { cn } from "cn";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { interactiveCard, stretchedLink } from "@/components/link-card";
import { Badge } from "@/components/ui/badge";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { Offer } from "@/features/actions";

// OfferCard is something in the self-service catalog, as a card whose
// title opens its form, the way link cards open their page.
export function OfferCard({ offer, onOpen }: Readonly<{ offer: Offer; onOpen: () => void }>) {
  return (
    <Card className={cn("h-full", interactiveCard)}>
      <CardHeader>
        <div className="flex items-start gap-3">
          <IconTile icon={offer.icon} color={offer.color} />
          <div className="grid min-w-0 flex-1 gap-1">
            <div className="flex items-center gap-2">
              <CardTitle className="truncate">
                <button type="button" onClick={onOpen} className={cn("text-left", stretchedLink)}>
                  {offer.title}
                </button>
              </CardTitle>
              {offer.destructive && (
                <Badge variant="destructive" className="ml-auto">
                  Destructive
                </Badge>
              )}
            </div>
            {offer.description && (
              <CardDescription className="line-clamp-2">{offer.description}</CardDescription>
            )}
            {/* An action asks for its entity first, which only this says. */}
            {offer.kind === "action" && offer.blueprintTitle && (
              <Badge variant="outline" className="mt-1">
                on {/^[aeiou]/i.test(offer.blueprintTitle) ? "an" : "a"} {offer.blueprintTitle}
              </Badge>
            )}
          </div>
        </div>
      </CardHeader>
    </Card>
  );
}
