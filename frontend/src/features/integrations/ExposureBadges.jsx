import { AlertTriangle, CheckCircle2, Plus } from 'lucide-react';
import { cn } from '../../ui/cn';

/**
 * One platform's scanning state on one account:
 *
 *   on       switched on and its last check passed
 *   failing  switched on, but the last check did not pass - nothing is scanned
 *   off      not switched on
 */
export function exposureState(platform) {
  if (platform.connected) return 'on';
  if (platform.checks.length > 0) return 'failing';
  return 'off';
}

const STATE = {
  on: { icon: CheckCircle2, word: 'On' },
  failing: { icon: AlertTriangle, word: 'Check failed' },
  off: { icon: null, word: 'Off' },
};

/** GitHub and CodeCommit state for an account row, opening its settings when allowed. */
export function ExposureBadges({ account, onOpen }) {
  if (!account.connectedAt) {
    return <span className="text-[12px] text-ink-3">After the account connects</span>;
  }
  const github = exposureState(account.exposure.github);
  const codecommit = exposureState(account.exposure.codecommit);
  const none = github === 'off' && codecommit === 'off';

  if (none) {
    return onOpen ? (
      <button
        type="button"
        onClick={onOpen}
        className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-[12px] font-medium text-brand transition-colors hover:bg-info-soft"
        aria-label={`Set up exposed-credential scanning for ${account.accountId}`}
      >
        <Plus aria-hidden="true" className="size-3.5" />
        Set up
      </button>
    ) : (
      <span className="text-[12px] text-ink-3">Off</span>
    );
  }

  /* Two short lines like the other columns: the overall state, then which
     platforms it covers. */
  const platforms = [
    ['GitHub', github],
    ['CodeCommit', codecommit],
  ].filter(([, state]) => state !== 'off');
  const failing = platforms.filter(([, state]) => state === 'failing');
  const lead = failing.length ? STATE.failing : STATE.on;
  const LeadIcon = lead.icon;
  const body = (
    <span className="block min-w-0">
      <span className={cn('inline-flex items-center gap-1.5 text-[12.5px] font-medium', failing.length ? 'text-medium' : 'text-low')}>
        <LeadIcon aria-hidden="true" className="size-3.5" />
        {failing.length ? 'Check failed' : 'Scanning'}
      </span>
      <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">
        {platforms.map(([label, state]) => (state === 'failing' ? `${label} (failed)` : label)).join(' · ')}
      </span>
    </span>
  );
  return onOpen ? (
    <button
      type="button"
      onClick={onOpen}
      className="-mx-1.5 -my-1 block max-w-full min-w-0 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-surface-3"
      aria-label={`Exposed-credential scanning for ${account.accountId}: GitHub ${STATE[github].word}, CodeCommit ${STATE[codecommit].word}. Manage`}
    >
      {body}
    </button>
  ) : (
    body
  );
}
