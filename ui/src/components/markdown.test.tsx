import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";
import { Markdown } from "./markdown";

describe("markdown", () => {
  it("shows tables as GitHub does", () => {
    render(<Markdown>{"| Setting | Now |\n| --- | --- |\n| replicas | 2 |"}</Markdown>);

    expect(screen.getByRole("columnheader", { name: "Setting" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "replicas" })).toBeInTheDocument();
  });

  it("shows raw HTML as text and drops script links", () => {
    const { container } = render(
      <Markdown>
        {"<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[open](javascript:alert(1))"}
      </Markdown>,
    );

    expect(container.querySelector("script, img")).toBeNull();
    expect(container).toHaveTextContent("<script>alert(1)</script>");
    expect(container.querySelector("a")?.getAttribute("href")).not.toContain("javascript");
  });
});
