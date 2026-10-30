import { Children, Fragment, type ReactNode } from "react";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

type PageBreadcrumbsProps = {
  // children are the links to the pages above this one, outermost first.
  children: ReactNode;
  page: string;
};

// PageBreadcrumbs is the trail every page below a top-level page starts with.
export function PageBreadcrumbs({ children, page }: PageBreadcrumbsProps) {
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {Children.toArray(children).map((link, index) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: the trail never reorders.
          <Fragment key={index}>
            <BreadcrumbItem>
              <BreadcrumbLink asChild>{link}</BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
          </Fragment>
        ))}
        <BreadcrumbItem>
          <BreadcrumbPage className="max-w-64 truncate">{page}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  );
}
