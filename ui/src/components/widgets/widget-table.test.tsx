import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { TableBody, TableCell, TableRow } from "@/components/ui/table";
import { WidgetTable } from "./widget-table";

afterEach(() => {
  vi.restoreAllMocks();
});

const table = (
  <WidgetTable>
    <TableBody>
      <TableRow>
        <TableCell>sonar-scan</TableCell>
      </TableRow>
    </TableBody>
  </WidgetTable>
);

// edge is the fade that says more of the table is out of view.
const edge = (container: HTMLElement) => container.querySelector("[aria-hidden].bg-linear-to-l");

describe("WidgetTable", () => {
  it("fades its edge while more of it is out of view", () => {
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(601);
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(544);
    const { container } = render(table);
    expect(edge(container)).toBeInTheDocument();
  });

  it("has no fade when it fits", () => {
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(544);
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(544);
    const { container } = render(table);
    expect(edge(container)).not.toBeInTheDocument();
  });
});
