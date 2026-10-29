import { StateCard } from "@/components/common/state-card";
import type { IntegrationSummary } from "@/features/integrations";
import { IntegrationIcon } from "./icons";
import { IntegrationDetail } from "./integration-detail";
import { MasterDetail, SidebarItem } from "./master-detail";
import { RunStatusIcon } from "./run-status-icon";

export function IntegrationsTab({
  integrations,
  selected,
  onSelect,
}: Readonly<{
  integrations: IntegrationSummary[];
  selected: string | undefined;
  onSelect: (identifier: string) => void;
}>) {
  const defined = integrations.filter((integration) => !integration.manual);
  const current = defined.find((integration) => integration.identifier === selected) ?? defined[0];

  if (!current) {
    return (
      <StateCard
        title="No integrations"
        message="Define an integration in the definitions tab to feed the catalog."
      />
    );
  }

  return (
    <MasterDetail
      label="Integrations"
      sidebar={defined.map((integration) => (
        <SidebarItem
          key={integration.identifier}
          selected={integration.identifier === current.identifier}
          onSelect={() => onSelect(integration.identifier)}
          primary={
            <>
              <IntegrationIcon icon={integration.icon} />
              <span className="truncate font-medium">{integration.title}</span>
            </>
          }
          secondary={
            integration.last_run ? (
              <RunStatusIcon status={integration.last_run.status} />
            ) : (
              "Never run"
            )
          }
        />
      ))}
    >
      <IntegrationDetail key={current.identifier} identifier={current.identifier} />
    </MasterDetail>
  );
}
