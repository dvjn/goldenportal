import { createFileRoute } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { signoutUrl } from "@/features/auth";

export const Route = createFileRoute("/not-provisioned")({
  component: NotProvisioned,
});

function NotProvisioned() {
  return (
    <section className="flex min-h-screen flex-col items-center justify-center gap-4 p-4 text-center">
      <h1 className="text-2xl font-semibold">You don't have access to this portal yet</h1>
      <p className="text-muted-foreground">Contact your administrator to get access.</p>
      <Button asChild variant="outline">
        <a href={signoutUrl}>Sign out</a>
      </Button>
    </section>
  );
}
