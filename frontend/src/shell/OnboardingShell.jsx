import { TopBar } from './TopBar';

/**
 * The console before any AWS account is connected: the top bar (brand,
 * theme, account menu) and the setup, nothing else. No sidebar - every other
 * screen would be empty - and no search, for the same reason.
 */
export function OnboardingShell({ children }) {
  return (
    <div className="min-h-dvh bg-canvas">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-[90] focus:rounded-md focus:bg-surface focus:px-3 focus:py-2 focus:text-[13px] focus:font-medium focus:text-ink focus:shadow-lg"
      >
        Skip to content
      </a>
      <TopBar minimal />
      <main id="main" className="px-3 pt-20 pb-14 sm:px-5 lg:px-6">
        <div className="content-scale @container mx-auto w-full max-w-[1280px]">{children}</div>
      </main>
    </div>
  );
}
