import { type ErrorComponentProps, Link, useRouter } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { ForbiddenError } from "@/features/auth";

export function PageError({ error }: ErrorComponentProps) {
  const router = useRouter();

  if (error instanceof ForbiddenError) {
    return (
      <section className="flex flex-1 flex-col items-center justify-center gap-4 py-16 text-center">
        <p className="text-muted-foreground font-mono text-sm">403</p>
        <h1 className="text-2xl font-semibold">You don't have access to this page.</h1>
        <p className="text-muted-foreground">Ask an admin if you need it.</p>
        <Button asChild>
          <Link to="/">Back to home</Link>
        </Button>
      </section>
    );
  }

  return (
    <section className="flex flex-1 flex-col items-center justify-center gap-4 py-16 text-center">
      <h1 className="text-2xl font-semibold">Something went wrong</h1>
      <p className="text-muted-foreground">
        {error instanceof Error ? error.message : String(error)}
      </p>
      <Button variant="outline" onClick={() => router.invalidate()}>
        Try again
      </Button>
    </section>
  );
}
