import { type ErrorComponentProps, Link, useRouter } from "@tanstack/react-router";
import { HTTPError } from "ky";
import { CircleAlert, FileQuestion, Lock } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

type PageErrorProps = ErrorComponentProps & {
  // back links to the page above, for when this one doesn't exist.
  back?: ReactNode;
};

// PageError takes the place of a page that couldn't load, below the header,
// so the navigation stays usable.
export function PageError({ error, back }: PageErrorProps) {
  const router = useRouter();
  const status = error instanceof HTTPError ? error.response.status : undefined;

  if (status === 404) {
    return (
      <StateCard
        icon={<FileQuestion />}
        title="Not found"
        description="It may have been renamed or deleted."
      >
        <Button variant="outline" asChild>
          {back ?? <Link to="/">Go to Home</Link>}
        </Button>
      </StateCard>
    );
  }
  if (status === 403) {
    return (
      <StateCard
        icon={<Lock />}
        title="You don't have access to this page"
        description="Ask an admin if you need it."
      >
        <Button variant="outline" asChild>
          <Link to="/">Go to Home</Link>
        </Button>
      </StateCard>
    );
  }
  return (
    <StateCard
      icon={<CircleAlert />}
      title="Something went wrong"
      description={error instanceof Error ? error.message : String(error)}
    >
      <Button variant="outline" onClick={() => router.invalidate()}>
        Try again
      </Button>
    </StateCard>
  );
}

type StateCardProps = {
  icon: ReactNode;
  title: string;
  description: ReactNode;
  children?: ReactNode;
};

// StateCard explains why a page or section shows no content.
export function StateCard({ icon, title, description, children }: StateCardProps) {
  return (
    <Card className="py-0">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">{icon}</EmptyMedia>
          <EmptyTitle>{title}</EmptyTitle>
          <EmptyDescription>{description}</EmptyDescription>
        </EmptyHeader>
        {children && <EmptyContent>{children}</EmptyContent>}
      </Empty>
    </Card>
  );
}
