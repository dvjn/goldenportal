import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { type NavigateOptions, useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useState } from "react";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { Badge } from "@/components/ui/badge";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import { type SearchKind, type SearchResult, searchQueryOptions } from "@/features/search";
import { modKey, useShortcut } from "@/hooks/use-shortcut";

// searchDelay waits for a pause in typing before asking the server.
const searchDelay = 200;

// SearchCommand finds whatever the signed-in user can open, from anywhere:
// the sidebar's Search item or the platform's ⌘K / Ctrl+K open it, and
// picking a result opens its page.
export function SearchCommand() {
  const [open, setOpen] = useState(false);
  useShortcut("k", () => setOpen((was) => !was));

  return (
    <>
      <SidebarMenuItem>
        <SidebarMenuButton tooltip="Search" onClick={() => setOpen(true)}>
          <Search />
          <span>Search</span>
          <KbdGroup className="ml-auto group-data-[collapsible=icon]:hidden">
            <Kbd>{modKey}</Kbd>
            <Kbd>K</Kbd>
          </KbdGroup>
        </SidebarMenuButton>
      </SidebarMenuItem>
      <CommandDialog
        open={open}
        onOpenChange={setOpen}
        title="Search"
        description="Find anything in the portal by its title or identifier."
      >
        {/* Mounting only while open starts each search afresh. */}
        {open && <SearchResults onDone={() => setOpen(false)} />}
      </CommandDialog>
    </>
  );
}

// kinds says what each kind of result is called on its chip, and where it
// opens. The chip always names the kind; an entity's blueprint stands
// beside its title, since a blueprint can itself be called "Blueprint".
const kinds: Record<SearchKind, { label: string; open: (r: SearchResult) => NavigateOptions }> = {
  blueprint: {
    label: "Blueprint",
    open: (r) => ({ to: "/catalog/$blueprint", params: { blueprint: r.identifier } }),
  },
  entity: {
    label: "Entity",
    open: (r) => ({
      to: "/catalog/$blueprint/$",
      params: { blueprint: r.blueprint ?? "", _splat: r.identifier },
    }),
  },
  dashboard: { label: "Dashboard", open: (r) => ({ to: "/", search: { d: r.identifier } }) },
  scorecard: {
    label: "Scorecard",
    open: (r) => ({ to: "/scorecards/$identifier", params: { identifier: r.identifier } }),
  },
  integration: {
    label: "Integration",
    open: (r) => ({ to: "/integrations/$identifier", params: { identifier: r.identifier } }),
  },
  action: {
    label: "Action",
    open: (r) => ({ to: "/actions/$identifier", params: { identifier: r.identifier } }),
  },
  // Members find a workflow to run it, so it opens in the catalog's form.
  workflow: {
    label: "Workflow",
    open: (r) => ({ to: "/self-service", search: { workflow: r.identifier } }),
  },
};

function SearchResults({ onDone }: Readonly<{ onDone: () => void }>) {
  const navigate = useNavigate();
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => setQuery(text.trim()), searchDelay);
    return () => clearTimeout(timer);
  }, [text]);

  const { data, isFetching } = useQuery({
    ...searchQueryOptions(query),
    placeholderData: keepPreviousData,
  });
  const searching = query !== text.trim() || isFetching;
  // New results highlight their best match, so Enter opens it.
  const [selected, setSelected] = useState("");
  const first = data?.[0];
  const best = first ? itemValue(first) : "";
  useEffect(() => setSelected(best), [best]);

  return (
    // The server matches and ranks every kind together, closest first, so
    // the list shows results as given rather than filtering them again.
    <Command shouldFilter={false} value={selected} onValueChange={setSelected}>
      <CommandInput value={text} onValueChange={setText} placeholder="Search the portal" />
      <CommandList>
        <CommandEmpty>{searching ? "Searching…" : "No results"}</CommandEmpty>
        {/* One unheaded group gives the list the stock padding. */}
        <CommandGroup>
          {data?.map((result) => (
            <CommandItem
              key={itemValue(result)}
              value={itemValue(result)}
              onSelect={() => {
                onDone();
                void navigate(kinds[result.kind].open(result));
              }}
            >
              <IconTile icon={result.icon} color={result.color} size="sm" />
              <span className="truncate">{result.title}</span>
              {result.detail && (
                <span className="shrink-0 text-xs text-muted-foreground">{result.detail}</span>
              )}
              <CommandShortcut className="tracking-normal">
                <Badge variant="outline">{kinds[result.kind].label}</Badge>
              </CommandShortcut>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </Command>
  );
}

function itemValue(result: SearchResult) {
  return `${result.kind}:${result.blueprint ?? ""}/${result.identifier}`;
}
