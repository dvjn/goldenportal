import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";
import type { Property } from "@/features/blueprints";
import { PropertyValue } from "./property-value";

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

  it("shows the first few tags of a compact value and counts the rest", () => {
    const tags: Property = { identifier: "tags", title: "Tags", type: "array" };
    render(<PropertyValue property={tags} value={["a", "b", "c", "d", "e"]} compact />);

    expect(screen.getByText("c")).toBeVisible();
    expect(screen.queryByText("d")).not.toBeInTheDocument();
    expect(screen.getByText("+2")).toHaveAttribute("title", "d, e");
  });

  it("cuts long compact text short and keeps it on hover", () => {
    const notes: Property = { identifier: "notes", title: "Notes", type: "string" };
    const long = "a long note ".repeat(20).trim();
    render(<PropertyValue property={notes} value={long} compact />);

    expect(screen.getByText(long)).toHaveAttribute("title", long);
    expect(screen.getByText(long)).toHaveClass("truncate");
  });
});
