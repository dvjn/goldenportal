import { PageHeader } from "@/components/common/page-header";
import { BlueprintGrid } from "./blueprint-grid";
import { IntegrationsCard, RecentChangesCard } from "./home-cards";

export function CatalogHome() {
  return (
    <>
      <PageHeader title="Catalog" />
      <BlueprintGrid />
      <div className="grid gap-4 lg:grid-cols-2">
        <IntegrationsCard />
        <RecentChangesCard />
      </div>
    </>
  );
}
