import { useEffect, useRef, useState } from 'react';
import { Link, Navigate, useLocation, useNavigate } from 'react-router-dom';
import { AlertTriangle, ArrowRight, BadgeCheck, Clock3, Info, Lock, ShieldCheck, UserRound } from 'lucide-react';
import { useAuth } from '../../app/AuthContext';
import { Button } from '../../ui/Button';
import { Field, Input, PasswordInput } from '../../ui/Field';

const TRUST_MARKS = [
  { icon: ShieldCheck, label: 'SOC 2 Type II Certified' },
  { icon: BadgeCheck, label: 'ISO 27001 Compliant' },
  { icon: Clock3, label: '24/7 Monitoring' },
];

/**
 * Sign-in.
 *
 * Built to the supplied design rather than approximated from it: the canvas
 * colour, the action colour, the accent, the card and field fills and the type
 * scale were all sampled and measured off that artwork (see
 * `docs/UX-DECISIONS.md` §8), so this screen is the artwork, at any width.
 *
 * Two things in the reference are deliberately absent. "Forgot password?" and
 * the tenant-onboarding and demo links have no endpoint behind them anywhere in
 * the API, and a control that cannot do anything is worse on a sign-in screen
 * than a missing one.
 *
 * The card is light in both themes, so it locks its own subtree to the light
 * palette with `data-theme="light"`.
 */
export default function LoginPage() {
  const { signIn, isAuthenticated, expired, clearExpired, user } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  /* "Switch account" from the account menu: sign in as someone else in this
     tab while other tabs keep theirs - how two roles are worked side by side
     in one browser. */
  const switching = new URLSearchParams(location.search).get('switch') === '1';

  /* Starts blank. People sign in with their work email (name@company.com) or
     their workspace username (name@workspace, e.g. cirm@bimasugam); both are
     the same login id to the API, which checks the pair. */
  const [values, setValues] = useState({ email: '', password: '' });
  const [fieldErrors, setFieldErrors] = useState({});
  const [formError, setFormError] = useState(null);
  const [submitting, setSubmitting] = useState(false);
  const usernameRef = useRef(null);
  const submitRef = useRef(null);

  /* Focus the username field: the form starts empty, so the next thing
     anybody does is type into it. */
  useEffect(() => {
    usernameRef.current?.focus();
  }, []);

  if (isAuthenticated && !switching) {
    return <Navigate to={location.state?.from?.pathname || '/'} replace />;
  }

  const onSubmit = async (event) => {
    event.preventDefault();
    setFormError(null);

    const errors = {};
    const login = values.email.trim();
    if (!login) errors.email = 'Enter your email or username.';
    else if (/\s/.test(login)) errors.email = 'Your email or username has no spaces.';
    if (!values.password) errors.password = 'Enter your password.';
    setFieldErrors(errors);
    if (Object.keys(errors).length > 0) return;

    setSubmitting(true);
    try {
      clearExpired();
      await signIn({ email: values.email.trim(), password: values.password });
      navigate(location.state?.from?.pathname || '/', { replace: true });
    } catch (error) {
      setFormError(error?.message || 'Sign-in failed.');
      setValues((v) => ({ ...v, password: '' }));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="auth-canvas relative overflow-hidden">
      {/* Both columns start on the same line. `items-start` rather than
          `items-center` is what makes that true - centring each column
          independently is what pushed the statement below the card. */}
      <main className="relative mx-auto grid min-h-[inherit] w-full max-w-[1900px] grid-cols-1 content-center items-start gap-x-[clamp(2rem,4vw,6rem)] gap-y-12 px-[clamp(1.25rem,5.9vw,7rem)] py-[clamp(2rem,3.6vh,3.5rem)] lg:grid-cols-[minmax(0,1fr)_clamp(23rem,32.7vw,38.75rem)]">
        {/* Product statement. No `order` override on either column: source
           order is statement then card, which stacks that way on a phone and
           reads left-to-right on a wide screen. */}
        <section>
          <p
            style={{ '--step': 1 }}
            className="animate-auth inline-flex w-fit items-center gap-2.5 rounded-full bg-[var(--t-auth-badge)] px-[clamp(0.9rem,1.1vw,1.35rem)] py-[clamp(0.4rem,0.55vw,0.65rem)] text-[clamp(10px,0.79vw,15px)] font-semibold tracking-[0.15em] text-[var(--t-auth-badge-ink)] uppercase"
          >
            <span aria-hidden="true" className="size-[0.45em] rounded-full bg-[var(--t-auth-accent)]" />
            Enterprise Security Platform
          </p>

          <h1
            style={{ '--step': 2 }}
            className="animate-auth mt-[clamp(1.5rem,2.6vw,3.25rem)] font-display text-[clamp(38px,4.86vw,92px)] leading-[1.08] font-extrabold tracking-[-0.035em] text-[var(--t-auth-ink)]"
          >
            Non-Human
            <br />
            Identity
            <br />
            <span className="text-[var(--t-auth-accent)]">Discovery</span>
          </h1>

          <p
            style={{ '--step': 3 }}
            className="animate-auth mt-[clamp(1.25rem,2vw,2.5rem)] max-w-[46ch] text-[clamp(14px,1.21vw,23px)] leading-[1.6] text-[var(--t-auth-ink-2)]"
          >
            Inventory every workload, pipeline and agent acting in your AWS accounts and the roles
            and keys they hold, trace what they can reach, and catch secrets the moment they leak
            into source control.
          </p>

          <ul
            style={{ '--step': 4 }}
            className="animate-auth mt-[clamp(1.75rem,2.8vw,3.5rem)] flex flex-wrap items-center gap-x-[clamp(1.25rem,2vw,2.5rem)] gap-y-3"
          >
            {TRUST_MARKS.map((mark) => (
              <li
                key={mark.label}
                className="flex items-center gap-2.5 text-[clamp(12.5px,1.06vw,20px)] text-[var(--t-auth-ink-2)]"
              >
                <mark.icon aria-hidden="true" className="size-[1.15em] shrink-0 text-[var(--t-auth-accent)]" />
                {mark.label}
              </li>
            ))}
          </ul>

          <p
            style={{ '--step': 5 }}
            className="animate-auth mt-[clamp(2.5rem,4vw,5rem)] text-[clamp(11.5px,0.9vw,17px)] text-[var(--t-auth-ink-2)]"
          >
            © {new Date().getFullYear()} Deep Algorithms · Fostering AI. Connecting Minds.
          </p>
        </section>

        {/* Credential card - floats on the navy, light in both themes. */}
        <section data-theme="light" className="w-full">
          <div
            /* Stacked, the card is last, so it enters last; side by side it
               enters with the headline. Otherwise the element at the bottom of
               a phone screen would animate before the ones above it. */
            className="animate-auth-card [--step:6] rounded-[clamp(14px,1.05vw,20px)] border border-[var(--t-auth-card-line)] bg-[var(--t-auth-card)] p-[clamp(1.5rem,2.85vw,3.4rem)] shadow-[0_34px_80px_-28px_rgba(2,10,20,0.55)] lg:[--step:2]"
          >
            {/* The supplied design uses the tagline lockup, centred. The asset
                is delivered on a 16:9 canvas, so the frame crops its symmetric
                padding rather than the file being edited. */}
            <div className="flex justify-center">
              <span
                className="block h-[clamp(34px,2.96vw,56px)] overflow-hidden"
                style={{ aspectRatio: '8000 / 2044' }}
              >
                <img
                  src="/brand/logo-lockup-tagline.png"
                  alt="Deep Algorithms"
                  className="size-full object-cover"
                  draggable="false"
                />
              </span>
            </div>

            <h2 className="mt-[clamp(1.5rem,2.6vw,3.1rem)] font-display text-[clamp(24px,2.11vw,40px)] leading-tight font-extrabold tracking-[-0.028em] text-ink">
              Welcome
            </h2>
            <p className="mt-[clamp(0.25rem,0.5vw,0.6rem)] text-[clamp(13px,1.06vw,20px)] text-ink-3">
              Sign in to access the platform
            </p>

            {switching && isAuthenticated && (
              <div
                role="status"
                className="mt-5 flex items-start gap-2 rounded-[var(--radius-control)] border border-info/25 bg-info-soft px-3 py-2.5 text-[clamp(12px,0.85vw,16px)] text-ink-2"
              >
                <Info aria-hidden="true" className="mt-px size-4 shrink-0 text-info" />
                <span>
                  Signed in as {user?.name ?? 'another account'} in this tab. Signing in here switches this tab
                  only - other open tabs keep their account.{' '}
                  <Link to="/" className="font-semibold text-brand underline-offset-4 hover:underline">
                    Back to the console
                  </Link>
                </span>
              </div>
            )}

            {expired && !formError && (
              <p
                role="status"
                className="mt-5 flex items-start gap-2 rounded-[var(--radius-control)] border border-medium/25 bg-medium-soft px-3 py-2.5 text-[clamp(12px,0.85vw,16px)] text-ink-2"
              >
                <AlertTriangle aria-hidden="true" className="mt-px size-4 shrink-0 text-medium" />
                Your session expired. Sign in again to continue where you left off.
              </p>
            )}

            {formError && (
              <p
                role="alert"
                className="mt-5 flex items-start gap-2 rounded-[var(--radius-control)] border border-critical/25 bg-critical-soft px-3 py-2.5 text-[clamp(12px,0.85vw,16px)] text-ink-2"
              >
                <AlertTriangle aria-hidden="true" className="mt-px size-4 shrink-0 text-critical" />
                {formError}
              </p>
            )}

            <form
              onSubmit={onSubmit}
              noValidate
              className="mt-[clamp(1.25rem,1.9vw,2.4rem)] flex flex-col gap-[clamp(1rem,1.6vw,2rem)]"
            >
              <Field
                label={
                  <span className="inline-flex items-center gap-2 text-[clamp(12px,0.9vw,17px)]">
                    <UserRound aria-hidden="true" className="size-[1.05em] text-ink-3" />
                    Email or username
                  </span>
                }
                htmlFor="login-username"
                error={fieldErrors.email}
              >
                <Input
                  id="login-username"
                  ref={usernameRef}
                  size="auth"
                  type="text"
                  inputMode="email"
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck="false"
                  placeholder="you@company.com or name@workspace"
                  value={values.email}
                  invalid={Boolean(fieldErrors.email)}
                  onChange={(event) => setValues((v) => ({ ...v, email: event.target.value }))}
                />
              </Field>

              <Field
                label={
                  <span className="inline-flex items-center gap-2 text-[clamp(12px,0.9vw,17px)]">
                    <Lock aria-hidden="true" className="size-[1.05em] text-ink-3" />
                    Password
                  </span>
                }
                htmlFor="login-password"
                error={fieldErrors.password}
              >
                <PasswordInput
                  id="login-password"
                  size="auth"
                  autoComplete="current-password"
                  placeholder="••••••••"
                  value={values.password}
                  invalid={Boolean(fieldErrors.password)}
                  onChange={(event) => setValues((v) => ({ ...v, password: event.target.value }))}
                />
              </Field>

              <Button
                ref={submitRef}
                type="submit"
                variant="auth"
                size="auth"
                className="mt-[clamp(0.25rem,0.6vw,0.75rem)]"
                loading={submitting}
                iconRight={ArrowRight}
              >
                {submitting ? 'Signing in…' : 'Sign in securely'}
              </Button>
            </form>

            <p className="mt-[clamp(1.25rem,1.9vw,2.4rem)] text-center text-[clamp(11px,0.82vw,15.5px)] leading-relaxed text-ink-3">
              Authorized use only. Every access attempt is logged and monitored.
            </p>
          </div>
        </section>
      </main>
    </div>
  );
}
