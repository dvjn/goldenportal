import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { StateCard } from "@/components/common/state-card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCan } from "@/features/auth";
import { blueprintsQueryOptions } from "@/features/catalog";
import { useDefinitions } from "@/features/definitions";
import { useIntegrations } from "@/features/integrations";
import { DataModelTab } from "./data-model-tab";
import { DefinitionsTab } from "./definitions-tab";
import { IntegrationsTab } from "./integrations-tab";

export const configurationTabs = ["model", "integrations", "definitions"] as const;

export type ConfigurationTab = (typeof configurationTabs)[number];

export type ConfigurationSearch = {
  tab?: ConfigurationTab;
  blueprint?: string;
  integration?: string;
  document?: string;
};

function isConfigurationTab(value: string): value is ConfigurationTab {
  return configurationTabs.some((tab) => tab === value);
}

function RevisionLink() {
  const definitions = useDefinitions();
  if (!definitions.data) return null;
  return (
    <Link
      to="/settings/catalog-setup/history"
      className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
    >
      Revision {definitions.data.revision}
    </Link>
  );
}

export function ConfigurationView({
  search,
  onSearchChange,
}: Readonly<{
  search: ConfigurationSearch;
  onSearchChange: (next: ConfigurationSearch) => void;
}>) {
  const can = useCan();
  const editor = can("catalog.model");
  const blueprints = useQuery(blueprintsQueryOptions);
  const integrations = useIntegrations();
  const tab = search.tab === "definitions" && !editor ? "model" : (search.tab ?? "model");

  let content = <ListSkeleton />;
  if (blueprints.isError || integrations.isError) {
    content = (
      <StateCard
        title="Catalog setup unavailable"
        message="We could not load the data model and integrations. Try again."
      />
    );
  } else if (blueprints.data && integrations.data) {
    content = (
      <Tabs
        value={tab}
        onValueChange={(next) => {
          if (isConfigurationTab(next)) onSearchChange({ ...search, tab: next });
        }}
      >
        <TabsList>
          <TabsTrigger value="model">Data model</TabsTrigger>
          <TabsTrigger value="integrations">Integrations</TabsTrigger>
          {editor ? <TabsTrigger value="definitions">Definitions</TabsTrigger> : null}
        </TabsList>
        <TabsContent value="model" className="flex flex-col gap-4">
          <DataModelTab
            blueprints={blueprints.data}
            selected={search.blueprint}
            onSelect={(blueprint) => onSearchChange({ ...search, blueprint })}
            onShowIntegration={(integration) =>
              onSearchChange({ ...search, tab: "integrations", integration })
            }
          />
        </TabsContent>
        <TabsContent value="integrations" className="flex flex-col gap-4">
          <IntegrationsTab
            integrations={integrations.data}
            selected={search.integration}
            onSelect={(integration) => onSearchChange({ ...search, integration })}
          />
        </TabsContent>
        {editor ? (
          <TabsContent value="definitions" className="flex flex-col gap-4">
            <DefinitionsTab
              blueprints={blueprints.data}
              integrations={integrations.data}
              selected={search.document}
              onSelect={(document) => onSearchChange({ ...search, document })}
            />
          </TabsContent>
        ) : null}
      </Tabs>
    );
  }

  return (
    <>
      <PageHeader
        title="Catalog setup"
        description="The data model, the integrations that feed it, and the definitions behind both."
        actions={editor ? <RevisionLink /> : null}
      />
      {content}
    </>
  );
}
