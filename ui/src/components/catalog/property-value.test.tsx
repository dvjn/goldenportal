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
});
