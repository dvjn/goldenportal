import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Property } from "@/features/blueprints";
import { PropertyValue } from "./property-value";

afterEach(() => {
  vi.restoreAllMocks();
});

const url: Property = { identifier: "url", title: "URL", type: "string", format: "uri" };

describe("PropertyValue", () => {
  it("links http(s) URLs", () => {
    render(<PropertyValue property={url} value="https://example.com/a" />);

    expect(screen.getByRole("link", { name: "example.com/a" })).toHaveAttribute(
      "href",
      "https://example.com/a",
    );
  });

  it("shows other URLs as text", () => {
    render(<PropertyValue property={url} value="javascript:alert(1)" />);

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("javascript:alert(1)")).toBeVisible();
  });

  it("shows a compact list's first item and lists up to ten more on demand", async () => {
    const user = userEvent.setup();
    const tags: Property = { identifier: "tags", title: "Tags", type: "array" };
    const values = Array.from({ length: 12 }, (_, i) => `tag${i + 1}`);
    render(<PropertyValue property={tags} value={values} compact />);

    expect(screen.getByText("tag1")).toBeVisible();
    expect(screen.queryByText("tag2")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show 11 more" }));
    expect(await screen.findByText("tag10")).toBeVisible();
    expect(screen.queryByText("tag11")).not.toBeInTheDocument();
  });

  it("shows a few of a list as chips and opens the rest in place", async () => {
    const user = userEvent.setup();
    const tags: Property = { identifier: "tags", title: "Tags", type: "array" };
    render(<PropertyValue property={tags} value={["a", "b", "c", "d", "e", "f", "g", "h"]} />);

    expect(screen.getByText("f")).toBeVisible();
    expect(screen.queryByText("g")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show 2 more" }));
    expect(screen.getByText("h")).toBeVisible();
    expect(screen.queryByRole("button", { name: /more/ })).not.toBeInTheDocument();
  });

  it("cuts a long value short in a cell and shows all of it on focus", async () => {
    const user = userEvent.setup();
    // jsdom lays nothing out, so say every cell overflows.
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(500);
    const notes: Property = { identifier: "notes", title: "Notes", type: "string" };
    const long = "a long note ".repeat(20).trim();
    render(
      <>
        <PropertyValue property={notes} value={long} compact />
        <PropertyValue property={url} value="https://example.com/a/long/path" compact />
      </>,
    );

    // Text alone takes focus; a link does, and shows its whole address.
    expect(screen.getByText(long)).toHaveClass("truncate");
    await user.tab();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(long);
    await user.tab();
    expect(screen.getByRole("link", { name: "example.com/a/long/path" })).toHaveFocus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent("https://example.com/a/long/path");
  });
});
