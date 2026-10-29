import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  ChevronDownIcon,
  ChevronUpIcon,
  EyeIcon,
  FilterXIcon,
  FunnelIcon,
  SearchIcon,
} from "lucide-react";
import { Fragment, useEffect, useState } from "react";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PaginationBar } from "@/components/common/pagination-bar";
import { StateCard } from "@/components/common/state-card";
import { TableFrame } from "@/components/common/table-frame";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { RelativeTime } from "@/features/settings";
import { type AuditEntry, type AuditQuery, actorKey, auditPageSize } from "./api";
import { AuditDetailsDialog } from "./audit-details-dialog";
import { describeDetails } from "./describe";
import { actionLabel, actorKindLabel, resourceKindLabel } from "./labels";
import { auditPageQueryOptions, useAuditFacets } from "./queries";
import { ActorLabel, actorName, ResourceLabel, TwoLineText } from "./subjects";

export type AuditSearch = {
  q?: string;
  actor?: string;
  actor_kind?: string;
  action?: string;
  resource_kind?: string;
  from?: string;
  to?: string;
  sort?: "asc";
  page?: number;
};

const filterKeys = ["q", "actor", "actor_kind", "action", "resource_kind", "from", "to"] as const;

function toQuery(search: AuditSearch): AuditQuery {
  return {
    page: search.page ?? 1,
    q: search.q,
    actor: search.actor ? [search.actor] : undefined,
    actor_kind: search.actor_kind ? [search.actor_kind] : undefined,
    action: search.action ? [search.action] : undefined,
    resource_kind: search.resource_kind ? [search.resource_kind] : undefined,
    from: search.from,
    to: search.to,
    sort: search.sort ?? "desc",
  };
}

type FilterOption = { value: string; label: string };
type FilterSection = { label?: string; options: FilterOption[] };

const kindPrefix = "kind=";
const actorPrefix = "actor=";

function actorFilterValue(search: AuditSearch) {
  if (search.actor) return actorPrefix + search.actor;
  if (search.actor_kind) return kindPrefix + search.actor_kind;
  return undefined;
}

function actorFilter(value: string | undefined): Partial<AuditSearch> {
  return {
    actor: value?.startsWith(actorPrefix) ? value.slice(actorPrefix.length) : undefined,
    actor_kind: value?.startsWith(kindPrefix) ? value.slice(kindPrefix.length) : undefined,
  };
}

export function AuditLogView({
  search,
  onSearchChange,
}: {
  search: AuditSearch;
  onSearchChange: (next: Partial<AuditSearch>) => void;
}) {
  const audit = useQuery({
    ...auditPageQueryOptions(toQuery(search)),
    placeholderData: keepPreviousData,
  });
  const facets = useAuditFacets();
  const [selected, setSelected] = useState<AuditEntry | null>(null);
  const filtered = filterKeys.some((key) => search[key]);

  function filter(next: Partial<AuditSearch>) {
    onSearchChange({ ...next, page: undefined });
  }

  if (audit.isPending) return <ListSkeleton />;
  if (audit.isError) {
    return <StateCard title="Audit log unavailable" message={audit.error.message} />;
  }

  const { entries, total } = audit.data;
  const actorSections: FilterSection[] = [
    {
      label: "Kind",
      options: (facets?.actor_kinds ?? []).map((kind) => ({
        value: kindPrefix + kind,
        label: actorKindLabel(kind),
      })),
    },
    {
      label: "Actor",
      options: (facets?.actors ?? []).map((actor) => ({
        value: actorPrefix + actorKey(actor),
        label: actor.email || actorName(actor),
      })),
    },
  ];

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput value={search.q ?? ""} onChange={(q) => filter({ q: q || undefined })} />
        <DateField
          label="From"
          value={search.from}
          onChange={(from) => filter({ from: from || undefined })}
        />
        <DateField
          label="To"
          value={search.to}
          onChange={(to) => filter({ to: to || undefined })}
        />
        {filtered && (
          <Button
            variant="outline"
            onClick={() => filter(Object.fromEntries(filterKeys.map((key) => [key, undefined])))}
          >
            <FilterXIcon />
            Clear filters
          </Button>
        )}
      </div>
      <TableFrame
        align="top"
        footer={
          <PaginationBar
            page={search.page ?? 1}
            pageSize={auditPageSize}
            total={total}
            onPageChange={(page) => onSearchChange({ page: page === 1 ? undefined : page })}
          />
        }
      >
        <TableHeader>
          <TableRow>
            <SortHead
              label="When"
              ascending={search.sort === "asc"}
              onToggle={() => filter({ sort: search.sort === "asc" ? undefined : "asc" })}
            />
            <FilterHead
              label="Actor"
              value={actorFilterValue(search)}
              sections={actorSections}
              onSelect={(value) => filter(actorFilter(value))}
            />
            <FilterHead
              label="Action"
              value={search.action}
              sections={[
                {
                  options: (facets?.actions ?? []).map((action) => ({
                    value: action,
                    label: actionLabel(action),
                  })),
                },
              ]}
              onSelect={(action) => filter({ action })}
            />
            <FilterHead
              label="Resource"
              value={search.resource_kind}
              sections={[
                {
                  options: (facets?.resource_kinds ?? []).map((kind) => ({
                    value: kind,
                    label: resourceKindLabel(kind),
                  })),
                },
              ]}
              onSelect={(resource_kind) => filter({ resource_kind })}
            />
            <TableHead className="text-right">Details</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((entry) => (
            <TableRow key={entry.id}>
              <TableCell className="leading-5 text-muted-foreground">
                <RelativeTime at={entry.at} />
              </TableCell>
              <TableCell>
                <ActorLabel actor={entry.actor} />
              </TableCell>
              <TableCell>
                <TwoLineText title={actionLabel(entry.action)} subtitle={describeDetails(entry)} />
              </TableCell>
              <TableCell>
                <ResourceLabel entry={entry} />
              </TableCell>
              <TableCell className="text-right">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="-my-1 align-top"
                  aria-label="Show the entry"
                  onClick={() => setSelected(entry)}
                >
                  <EyeIcon />
                </Button>
              </TableCell>
            </TableRow>
          ))}
          {entries.length === 0 && (
            <TableRow>
              <TableCell colSpan={5} className="h-28 text-center text-muted-foreground">
                {filtered ? "No audit entry matches the filters." : "Nothing recorded yet."}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </TableFrame>
      <AuditDetailsDialog entry={selected} onClose={() => setSelected(null)} />
    </>
  );
}

function SearchInput({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const [draft, setDraft] = useState(value);

  useEffect(() => setDraft(value), [value]);

  useEffect(() => {
    if (draft === value) return;
    const timer = setTimeout(() => onChange(draft.trim()), 300);
    return () => clearTimeout(timer);
  }, [draft, value, onChange]);

  return (
    <InputGroup className="sm:max-w-xs">
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      <InputGroupInput
        aria-label="Search the audit log"
        placeholder="Search actor, action, resource, IP"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
      />
    </InputGroup>
  );
}

function DateField({
  label,
  value,
  onChange,
}: {
  label: string;
  value?: string;
  onChange: (value: string) => void;
}) {
  const id = `audit-${label.toLowerCase()}`;

  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={id} className="text-sm text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        type="date"
        className="w-40"
        value={value ?? ""}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

function SortHead({
  label,
  ascending,
  onToggle,
}: {
  label: string;
  ascending: boolean;
  onToggle: () => void;
}) {
  return (
    <TableHead>
      <div className="flex items-center gap-2">
        <span>{label}</span>
        <Button
          variant="toggle"
          size="icon-sm"
          aria-label={ascending ? "Sort newest first" : "Sort oldest first"}
          aria-pressed={ascending}
          onClick={onToggle}
        >
          {ascending ? (
            <ChevronUpIcon className="size-3.5 text-primary" />
          ) : (
            <ChevronDownIcon className="size-3.5" />
          )}
        </Button>
      </div>
    </TableHead>
  );
}

function FilterHead({
  label,
  value,
  sections,
  onSelect,
}: {
  label: string;
  value?: string;
  sections: FilterSection[];
  onSelect: (value: string | undefined) => void;
}) {
  const shown = sections.filter((section) => section.options.length > 0);

  return (
    <TableHead>
      <div className="flex items-center gap-2">
        <span>{label}</span>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="toggle"
              size="icon-sm"
              aria-label={`Filter by ${label.toLowerCase()}`}
              aria-pressed={Boolean(value)}
            >
              <FunnelIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="max-h-80 w-64 overflow-auto">
            <DropdownMenuRadioGroup
              value={value ?? ""}
              onValueChange={(next) => onSelect(next || undefined)}
            >
              <DropdownMenuRadioItem value="">Any</DropdownMenuRadioItem>
              {shown.map((section) => (
                <Fragment key={section.label ?? "options"}>
                  {section.label && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuLabel>{section.label}</DropdownMenuLabel>
                    </>
                  )}
                  {section.options.map((option) => (
                    <DropdownMenuRadioItem key={option.value} value={option.value}>
                      {option.label}
                    </DropdownMenuRadioItem>
                  ))}
                </Fragment>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </TableHead>
  );
}
