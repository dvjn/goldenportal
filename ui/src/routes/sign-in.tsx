import { createFileRoute, redirect } from "@tanstack/react-router";
import { AuthPage } from "@/components/auth-page";
import { LoginForm } from "@/components/login-form";
import { meQueryOptions, safeRedirect } from "@/features/auth";

type SignInSearch = {
  redirect?: string;
  error?: string;
};

export const Route = createFileRoute("/sign-in")({
  validateSearch: (search: Record<string, unknown>): SignInSearch => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const signedIn = await context.queryClient
      .ensureQueryData(meQueryOptions)
      .then(() => true)
      .catch(() => false);
    if (signedIn) {
      throw redirect({ href: safeRedirect(search.redirect) });
    }
  },
  component: SignInPage,
});

function SignInPage() {
  const { redirect, error } = Route.useSearch();

  return (
    <AuthPage>
      <LoginForm redirect={redirect} error={error} />
    </AuthPage>
  );
}
