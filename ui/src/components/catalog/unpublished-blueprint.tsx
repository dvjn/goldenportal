import { Link } from "@tanstack/react-router";
import { FileClock } from "lucide-react";
import { EmptyCard } from "@/components/empty-state";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { hasRole, useMe } from "@/features/auth";

// UnpublishedBlueprint stands in for a catalog or entity page whose
// blueprint has no published version yet, under the page's own header.
// Builders get a way to it; anyone else waits for it.
export function UnpublishedBlueprint({
  blueprint,
  title,
  page = title,
}: Readonly<{ blueprint: string; title: string; page?: string }>) {
  const builder = hasRole(useMe().role, "builder");
  return (
    <>
      <PageHeader
        title={page}
        breadcrumbs={
          <PageBreadcrumbs page={page}>
            <Link to="/catalog">Catalog</Link>
            {page !== title && (
              <Link to="/catalog/$blueprint" params={{ blueprint }}>
                {title}
              </Link>
            )}
          </PageBreadcrumbs>
        }
      />
      <EmptyCard
        icon={<FileClock />}
        title={`${title} isn't published yet`}
        description="Its entities show up here once it is."
      >
        {builder && (
          <Button variant="outline" asChild>
            <Link to="/blueprints/$identifier" params={{ identifier: blueprint }}>
              View Blueprint
            </Link>
          </Button>
        )}
      </EmptyCard>
    </>
  );
}
