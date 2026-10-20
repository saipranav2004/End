import { useAccess } from '../../app/useAccess';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  GitBranch,
  History,
  Play,
  RefreshCw,
  Radar,
} from 'lucide-react';
import { fetchDeepScanStatus, startDeepScan } from '../../lib/api/exposure';
import { normalisePlatform, platformMeta } from '../../lib/domain';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Field, Input, SearchInput } from '../../ui/Field';
import { Drawer } from '../../ui/Overlay';
import { SectionLabel } from '../../ui/Panel';
import { ClearState, EmptyState } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import {
  DEEP_SCAN_POLL_MS,
  deepScanStateMeta,
  describeDeepScanFailure,
  describeDeepScanRequestError,
} from './scannerState';

/**
 * Deep scan.
 *
 * ── Why a drawer and not a screen of its own ────────────────────────────────
 * A deep scan is a thing you do to a repository you are already looking at, so
 * it belongs beside the findings rather than in the navigation as a peer of
 * them. It also has no state of its own to come back to: once a walk finishes
 * its output is findings, and findings already have a screen.
 *
 * The repository list is passed in rather than fetched. The findings have
 * already been loaded by the screen behind this, and distinct
 * (client_id, repository) pairs are derivable from it - a second identical
 * request would only introduce a way for the two lists to disagree.
 *
 * ── What this is for ────────────────────────────────────────────────────────
 * The onboarding scan reads a repository as it exists right now. That means a
 * secret which was committed and later deleted is invisible to it - the file
 * no longer contains the value, but every clone of the repository still does,
 * and so does every fork. A deep scan walks the history commit by commit from
 * the first one to HEAD, which is the only way to find those.
 *
 * It is opt-in on the service's side because it does not scale automatically
 * to a repository with a long history, so this screen is a request-and-watch
 * screen rather than something that runs on a schedule.
 *
 * ── Why it polls ────────────────────────────────────────────────────────────
 * `POST /api/v1/credential-exposure/deep-scan` answers 202 "accepted", not
 * "done", and there is no push notification for completion. Polling the
 * status endpoint is the only mechanism there is, every five seconds.
 *
 * The API writes the request into the tenant's deep-scan table, which the
 * scanner worker picks up. Only rows that are actually running are polled: a completed
 * scan does not change, and re-asking for it every five seconds would be
 * traffic that answers nothing.
 */
export function DeepScanDrawer({ open, onClose, findings = [] }) {
  const { notify } = useToast();
  const [search, setSearch] = useState('');
  const [manualClient, setManualClient] = useState('');
  const [manualRepo, setManualRepo] = useState('');
  const [manualError, setManualError] = useState('');
  /* Repositories the operator named by hand, so a repository with no findings
     yet - which is exactly the case a deep scan is for - can still be asked
     for. Kept beside the discovered list rather than merged into it, because
     the two have different provenance and the reader should be able to tell. */
  const [manualTargets, setManualTargets] = useState([]);
  /* Set when the server says deep scans are not configured (503). */

  /* One row per repository the service has ever reported a finding against.
     Distinct (client_id, repository), which is the pair the two deep-scan
     endpoints identify a scan by - not the repository alone, because the same
     repository name can exist under two clients. */
  const discovered = useMemo(() => {
    const rows = new Map();
    for (const finding of findings) {
      if (!finding.client_id || !finding.repository) continue;
      const key = `${finding.client_id}␟${finding.repository}`;
      const existing = rows.get(key);
      if (existing) {
        existing.findings += 1;
        continue;
      }
      rows.set(key, {
        key,
        clientId: finding.client_id,
        repository: finding.repository,
        platform: normalisePlatform(finding.platform),
        findings: 1,
        source: 'findings',
      });
    }
    return [...rows.values()].sort(
      (a, b) => b.findings - a.findings || a.repository.localeCompare(b.repository),
    );
  }, [findings]);

  const targets = useMemo(() => {
    const seen = new Set(discovered.map((row) => row.key));
    return [...discovered, ...manualTargets.filter((row) => !seen.has(row.key))];
  }, [discovered, manualTargets]);

  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return targets;
    return targets.filter((row) =>
      `${row.repository} ${row.clientId}`.toLowerCase().includes(needle),
    );
  }, [targets, search]);

  const { statuses, refresh, request, pending } = useDeepScanStatuses(targets, open);

  const onRequest = useCallback(
    async (row) => {
      try {
        const accepted = await request(row);
        notify(
          accepted?.already_running
            ? {
                title: 'Already requested',
                description: `${row.repository} is already waiting for or in a history walk. This screen will follow it.`,
                variant: 'info',
              }
            : {
                title: 'Deep scan requested',
                description: `${row.repository} will be walked commit by commit. This screen will follow it.`,
                variant: 'success',
              },
        );
      } catch (error) {
        const described = describeDeepScanRequestError(error);
        notify({
          title: described.title,
          description: described.message,
          variant: 'error',
        });
      }
    },
    [request, notify],
  );

  const onAddManual = useCallback(
    (event) => {
      event.preventDefault();
      const clientId = manualClient.trim();
      const repository = manualRepo.trim();
      if (!clientId || !repository) {
        setManualError('Both a client id and a repository are required.');
        return;
      }
      const key = `${clientId}␟${repository}`;
      if (targets.some((row) => row.key === key)) {
        setManualError('That repository is already on the list below.');
        return;
      }
      setManualError('');
      /* No platform is claimed for a hand-entered row. Which platform a client
         is on comes from the service's own records, and guessing it from the
         presence of a slash would put a label on screen this dashboard cannot
         stand behind - the service will reject a wrong shape and say so. */
      setManualTargets((current) => [
        ...current,
        { key, clientId, repository, platform: null, findings: 0, source: 'manual' },
      ]);
      setManualClient('');
      setManualRepo('');
    },
    [manualClient, manualRepo, targets],
  );

  const counts = useMemo(() => {
    const out = { running: 0, complete: 0, failed: 0, not_started: 0 };
    for (const row of targets) {
      const state = String(statuses[row.key]?.data?.status || 'not_started').toLowerCase();
      if (state in out) out[state] += 1;
    }
    return out;
  }, [targets, statuses]);

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="xl"
      eyebrow="Credential exposure"
      title="Deep scan"
      subtitle="Walk a repository's whole history, not just the files as they are now."
      footer={
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-[11.5px] text-ink-3">
            {counts.running > 0
              ? `Polling every ${DEEP_SCAN_POLL_MS / 1000} seconds while a walk is running.`
              : 'Nothing is running, so nothing is being polled.'}
          </p>
          <Button variant="secondary" onClick={onClose}>
            Close
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        {/* Why anybody would run this, before the controls that run it. A
            control whose cost is "reads every commit you have ever pushed"
            should not be offered without saying what it buys. */}
        <div className="grid gap-3 @min-[34rem]:grid-cols-2">
          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
            <SectionLabel>The scan that already runs</SectionLabel>
            <p className="mt-1.5 text-[12.5px] leading-relaxed text-ink-2">
              Reads the files as they exist at HEAD. It catches a secret that is in the code now,
              which is most of them, and it is cheap enough to run on every push.
            </p>
          </div>
          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
            <SectionLabel>What a deep scan adds</SectionLabel>
            <p className="mt-1.5 text-[12.5px] leading-relaxed text-ink-2">
              Reads every commit from the first one to HEAD. It catches the secret somebody
              committed and then deleted in the next commit - still present in every clone and
              every fork, and the one people believe they have already fixed.
            </p>
          </div>
        </div>

        {/* Four counts rather than four tiles: inside a drawer the tile
            treatment competes with the list that matters. */}
        <dl className="flex flex-wrap gap-x-6 gap-y-2 rounded-[var(--radius-control)] border border-line bg-inset px-3.5 py-2.5 text-[12px]">
          <Fact label="Repositories" value={formatNumber(targets.length)} />
          <Fact label="Running" value={formatNumber(counts.running)} />
          <Fact label="History fully read" value={formatNumber(counts.complete)} />
          <Fact label="Failed" value={formatNumber(counts.failed)} />
        </dl>

        <div className="overflow-hidden rounded-[var(--radius-panel)] border border-line">
          <div className="flex flex-wrap items-center gap-3 border-b border-line bg-surface-2 px-3.5 py-2.5">
            <SearchInput
              size="sm"
              value={search}
              onChange={setSearch}
              placeholder="Search repository or client id…"
              className="w-full min-w-0 sm:max-w-xs"
            />
            <p className="text-[12px] text-ink-3" data-numeric="">
              {formatNumber(visible.length)} of {formatNumber(targets.length)}
            </p>
          </div>

          {visible.length === 0 ? (
            targets.length === 0 ? (
              <ClearState
                compact
                title="No repositories to scan"
                description="The scanner has not reported a finding against any repository yet, so there is nothing here to walk. A repository can still be named by hand below."
              />
            ) : (
              <EmptyState
                compact
                icon={Radar}
                title="No repository matches that search"
                description="Clear the search to see every repository the scanner knows about."
              />
            )
          ) : (
            <ul className="divide-y divide-line">
              {visible.map((row) => (
                <TargetRow
                  key={row.key}
                  row={row}
                  state={statuses[row.key]}
                  pending={pending === row.key}
                  onRequest={() => onRequest(row)}
                  onRefresh={() => refresh(row)}
                />
              ))}
            </ul>
          )}
        </div>

        {/* A repository with zero findings never appears above - and that is
            exactly the repository somebody wants to deep scan, because a clean
            snapshot says nothing about the history behind it. */}
        <div className="rounded-[var(--radius-panel)] border border-line bg-surface-2 p-3.5">
          <SectionLabel>Scan a repository that has no findings yet</SectionLabel>
          <p className="mt-1 text-[12px] leading-relaxed text-ink-3">
            The list above is built from findings, so a repository the scanner has never flagged is
            not on it.
          </p>
          <form className="mt-3 flex flex-wrap items-end gap-3" onSubmit={onAddManual}>
            <Field label="Client id" htmlFor="deep-client" className="min-w-[12rem] flex-1">
              <Input
                id="deep-client"
                value={manualClient}
                onChange={(event) => setManualClient(event.target.value)}
                placeholder="e.g. test-sample-client"
              />
            </Field>
            <Field
              label="Repository"
              htmlFor="deep-repo"
              className="min-w-[13rem] flex-1"
              hint="A bare name for CodeCommit, owner/repo for GitHub."
            >
              <Input
                id="deep-repo"
                value={manualRepo}
                onChange={(event) => setManualRepo(event.target.value)}
                placeholder="e.g. test-sample"
              />
            </Field>
            <Button type="submit" variant="secondary">
              Add
            </Button>
          </form>
          {manualError && <p className="mt-2 text-[12.5px] text-critical">{manualError}</p>}
          <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">
            Which platform a client is on comes from the scanner's own records, so nothing here says
            which one to use. Give the repository in the wrong shape and the service answers with
            which shape it wanted.
          </p>
        </div>
      </div>
    </Drawer>
  );
}

/**
 * One repository, its deep-scan state, and the one control it offers.
 *
 * The button's label is the state, in words: "Request deep scan" the first
 * time, "Scan again" once there is a result. A scan that is already running
 * offers nothing - re-posting would reset it back to the beginning, and
 * somebody watching a progress row would read that as a failure.
 */
function TargetRow({ row, state, pending, onRequest, onRefresh }) {
  const { lock } = useAccess();
  const status = state?.data;
  const meta = deepScanStateMeta(status?.status);
  const running = String(status?.status).toLowerCase() === 'running';
  const failed = String(status?.status).toLowerCase() === 'failed';
  const platform = row.platform ? platformMeta(row.platform) : null;
  /* The service reports which platform it holds this client under, which is
     better than anything the dashboard could infer - so once a status has come
     back it overrides the label derived from the findings. */
  const reported = status?.platform ? platformMeta(status.platform) : null;

  return (
    <li className="px-4 py-3.5">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2.5">
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <GitBranch aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
            <p className="min-w-0 truncate text-[13px] font-semibold text-ink" title={row.repository}>
              {row.repository}
            </p>
            {(reported ?? platform) && (
              <Tag tone={(reported ?? platform).tone} size="sm">
                {(reported ?? platform).label}
              </Tag>
            )}
            {row.source === 'manual' && (
              <Tag tone="neutral" size="sm">
                Added by hand
              </Tag>
            )}
          </div>
          <p className="mt-1 truncate font-mono text-[11.5px] text-ink-3">{row.clientId}</p>
          <p className="mt-1 text-[11.5px] leading-relaxed text-ink-3">
            {row.findings > 0
              ? `${formatNumber(row.findings)} live finding${row.findings === 1 ? '' : 's'} from the snapshot scan.`
              : 'No findings from the snapshot scan.'}{' '}
            {meta.description}
          </p>
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Tag tone={meta.tone} size="sm" dot={meta.tone !== 'neutral'}>
            {running ? (
              <span className="flex items-center gap-1.5">
                <span aria-hidden="true" className="deep-scan-pulse" />
                {meta.label}
              </span>
            ) : (
              meta.label
            )}
          </Tag>
          {/* A plain button, not an icon-only one: "re-check" is not a glyph
              anybody recognises, and this row already carries a primary
              action whose label the reader has to tell it apart from. */}
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={onRefresh}
            loading={state?.loading}
            aria-label={`Re-check the deep scan status for ${row.repository}`}
          >
            Re-check
          </Button>
          <Button
            variant={failed ? 'secondary' : 'primary'}
            size="sm"
            icon={status?.status && status.status !== 'not_started' ? History : Play}
            onClick={onRequest}
            loading={pending}
            disabled={running || pending}
            locked={running ? undefined : lock('exposure.deepScan')}
          >
            {running
              ? 'Running'
              : status?.status === 'complete'
                ? 'Scan again'
                : failed
                  ? 'Try again'
                  : 'Request deep scan'}
          </Button>
        </div>
      </div>

      {/* The numbers only exist once a walk has finished, so the row stays one
          line until there is something true to put on the second. */}
      {status && status.status !== 'not_started' && (
        <dl className="mt-2.5 flex flex-wrap gap-x-6 gap-y-1.5 rounded-[var(--radius-control)] bg-surface-2 px-3 py-2 text-[11.5px]">
          <Fact label="Requested" value={status.requested_at ? formatRelative(status.requested_at) : '-'} title={status.requested_at ? formatDateTime(status.requested_at) : undefined} />
          {status.completed_at && (
            <Fact label="Finished" value={formatRelative(status.completed_at)} title={formatDateTime(status.completed_at)} />
          )}
          {Number.isFinite(status.commits_scanned) && (
            <Fact label="Commits read" value={formatNumber(status.commits_scanned)} />
          )}
          {Number.isFinite(status.findings) && (
            <Fact label="Secrets found" value={formatNumber(status.findings)} />
          )}
        </dl>
      )}

      {status?.status === 'complete' && Number.isFinite(status.findings) && (
        <p className="mt-2 flex items-start gap-2 text-[12px] leading-relaxed text-ink-2">
          <CheckCircle2 aria-hidden="true" className="mt-px size-3.5 shrink-0 text-low" />
          {status.findings > 0 ? (
            <span>
              The history walk added {formatNumber(status.findings)} finding
              {status.findings === 1 ? '' : 's'}, which are in the live set on{' '}
              <Link to="/exposure" className="text-brand hover:underline">
                Exposed credentials
              </Link>
              .
            </span>
          ) : (
            <span>The whole history was read and no secret was found anywhere in it.</span>
          )}
        </p>
      )}

      {failed && <FailureNote status={status} />}

      {state?.error && !status && (
        <p className="mt-2 text-[12px] text-ink-3">
          {state.error.code === 'PLATFORM_REQUIRED'
            ? 'This client has a repository of this name on both GitHub and CodeCommit, so the status cannot be told apart without a platform.'
            : 'The status for this repository could not be read. It has not necessarily failed - only the check did.'}
        </p>
      )}
    </li>
  );
}

function Fact({ label, value, title }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <dt className="text-ink-3">{label}</dt>
      <dd className="font-medium text-ink-2" data-numeric="" title={title}>
        {value}
      </dd>
    </div>
  );
}

/**
 * A failed walk, explained before it is quoted.
 *
 * `error` is the raw exception the service caught. The integration guide is
 * explicit that it is fine in front of an operator and not something to show
 * an end user as-is, so it is put under a heading that says what it is - and
 * the cause is named above it, because the raw string says what threw without
 * saying what to do.
 */
function FailureNote({ status }) {
  const failure = describeDeepScanFailure(status);
  return (
    <div className="mt-2.5 rounded-[var(--radius-control)] border border-critical/30 bg-critical-soft p-3">
      <p className="flex items-start gap-2 text-[12.5px] leading-relaxed text-ink">
        <AlertTriangle aria-hidden="true" className="mt-px size-3.5 shrink-0 text-critical" />
        <span>
          <span className="font-semibold">{failure.cause}</span> {failure.fix}
        </span>
      </p>
      {status.failed_at && (
        <p className="mt-1.5 pl-5.5 text-[11.5px] text-ink-3">
          Failed {formatRelative(status.failed_at)} ({formatDateTime(status.failed_at)}).
        </p>
      )}
      {failure.raw && (
        <details className="mt-2 pl-5.5">
          <summary className="cursor-pointer text-[11.5px] font-medium text-ink-2">
            What the service reported
          </summary>
          <pre className="mt-1.5 overflow-x-auto rounded-[var(--radius-control)] border border-line bg-inset p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-ink-3">
            {failure.raw}
          </pre>
        </details>
      )}
    </div>
  );
}

/**
 * Statuses for every listed repository, polled while any of them is running.
 *
 * One interval for the whole page rather than one per row: a dozen timers
 * firing at slightly different moments produces a dozen requests a second
 * apart instead of one burst every five, and each row then re-renders on its
 * own schedule. The interval is torn down the moment nothing is running, so an
 * idle page makes no requests at all.
 *
 * Nothing happens while the drawer is closed. It is mounted all the time on the
 * findings screen, and an earlier version read a status for every repository
 * the moment that screen loaded - and kept polling any running scan in the
 * background - whether or not anybody ever opened the drawer. Every opening
 * re-reads every row, because a scan requested elsewhere, or one that finished
 * while the drawer was shut, would otherwise show its old state.
 */
function useDeepScanStatuses(targets, open) {
  const [statuses, setStatuses] = useState({});
  const [pending, setPending] = useState('');
  const targetsRef = useRef(targets);
  targetsRef.current = targets;

  const load = useCallback(async (row) => {
    setStatuses((current) => ({
      ...current,
      [row.key]: { ...current[row.key], loading: true },
    }));
    try {
      const data = await fetchDeepScanStatus({
        clientId: row.clientId,
        repository: row.repository,
        platform: row.platform || undefined,
      });
      setStatuses((current) => ({ ...current, [row.key]: { data, loading: false, error: null } }));
      return data;
    } catch (error) {
      if (error?.code === 'CANCELLED') return null;
      setStatuses((current) => ({
        ...current,
        [row.key]: { data: current[row.key]?.data, loading: false, error },
      }));
      return null;
    }
  }, []);

  /* Every listed repository is read once, so the list arrives with real states
     rather than a column of "not started" that turns out to be wrong. */
  const loadedRef = useRef(new Set());
  useEffect(() => {
    if (!open) {
      loadedRef.current = new Set();
      return;
    }
    for (const row of targets) {
      if (loadedRef.current.has(row.key)) continue;
      loadedRef.current.add(row.key);
      load(row);
    }
  }, [targets, load, open]);

  const runningKeys = targets
    .filter((row) => String(statuses[row.key]?.data?.status).toLowerCase() === 'running')
    .map((row) => row.key)
    .join(',');

  useEffect(() => {
    if (!open || !runningKeys) return undefined;
    const interval = setInterval(() => {
      const keys = new Set(runningKeys.split(','));
      for (const row of targetsRef.current) {
        if (keys.has(row.key)) load(row);
      }
    }, DEEP_SCAN_POLL_MS);
    return () => clearInterval(interval);
  }, [runningKeys, load, open]);

  /* Requesting a scan sets the row running immediately from the 202 body
     rather than waiting for the next poll, so the button's state changes on
     the click that caused it. */
  const request = useCallback(
    async (row) => {
      setPending(row.key);
      try {
        const accepted = await startDeepScan({ clientId: row.clientId, repository: row.repository, platform: row.platform });
        setStatuses((current) => ({
          ...current,
          [row.key]: {
            loading: false,
            error: null,
            data: {
              ...(current[row.key]?.data ?? {}),
              client_id: row.clientId,
              repo_key: row.repository,
              status: accepted?.status || 'running',
              platform: accepted?.platform ?? current[row.key]?.data?.platform,
              requested_at: accepted?.requested_at || new Date().toISOString(),
              /* A fresh request clears the previous run's result, which is what
                 the service does too - keeping the old numbers on screen would
                 attribute them to the run that just started. */
              completed_at: null,
              commits_scanned: null,
              findings: null,
              failed_at: null,
              error: null,
            },
          },
        }));
        return accepted;
      } finally {
        setPending('');
      }
    },
    [],
  );

  return { statuses, refresh: load, request, pending };
}

