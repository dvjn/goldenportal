import type { ReactNode } from "react";
import { Card } from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

type EmptyStateProps = {
  icon?: ReactNode;
  title: ReactNode;
  description: ReactNode;
  // children is what to do about it, such as the button that makes the first one.
  children?: ReactNode;
};

// EmptyState explains why a list or section shows nothing, inside whatever
// frames it, such as a table's card.
export function EmptyState({ icon, title, description, children }: EmptyStateProps) {
  return (
    <Empty>
      <EmptyHeader>
        {icon && <EmptyMedia variant="icon">{icon}</EmptyMedia>}
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      {children && <EmptyContent>{children}</EmptyContent>}
    </Empty>
  );
}

// EmptyCard is an EmptyState standing in for a page's content.
export function EmptyCard(props: EmptyStateProps) {
  return (
    <Card className="py-0">
      <EmptyState {...props} />
    </Card>
  );
}
