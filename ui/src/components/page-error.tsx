import { type ErrorComponentProps, Link, useRouter } from "@tanstack/react-router";
import { HTTPError } from "ky";
import { CircleAlert, FileQuestion, Lock } from "lucide-react";
import type { ReactNode } from "react";
import { EmptyCard } from "@/components/empty-state";
import { Button } from "@/components/ui/button";
import { ForbiddenError } from "@/features/auth";

type PageErrorProps = ErrorComponentProps & {
  // back links to the page above, for when this one doesn't exist.
  back?: ReactNode;
};

// PageError takes the place of a page that couldn't load, below the header,
// so the navigation stays usable.
export function PageError({ error, back }: PageErrorProps) {
  const router = useRouter();
  let status = error instanceof HTTPError ? error.response.status : undefined;
  if (error instanceof ForbiddenError) status = 403;

  if (status === 404) {
    return (
      <EmptyCard
        icon={<FileQuestion />}
        title="Not found"
        description="It may have been renamed or deleted."
      >
        <Button variant="outline" asChild>
          {back ?? <Link to="/">Go to Home</Link>}
        </Button>
      </EmptyCard>
    );
  }
  if (status === 403) {
    return (
      <EmptyCard
        icon={<Lock />}
        title="You don't have access to this page"
        description="Ask an admin if you need it."
      >
        <Button variant="outline" asChild>
          <Link to="/">Go to Home</Link>
        </Button>
      </EmptyCard>
    );
  }
  return (
    <EmptyCard
      icon={<CircleAlert />}
      title="Something went wrong"
      description={error instanceof Error ? error.message : String(error)}
    >
      <Button variant="outline" onClick={() => router.invalidate()}>
        Try again
      </Button>
    </EmptyCard>
  );
}
