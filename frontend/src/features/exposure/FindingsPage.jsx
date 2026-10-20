import { useCallback, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  ExternalLink,
  FileWarning,
  GitCommitHorizontal,
  List,
  Radar,
  SearchX,
  ShieldOff,
  Sparkles,
} from 'lucide-react';
import { useAuth } from '../../app/AuthContext';
import { decideFinding, dismissFinding, fetchFindings } from '../../lib/api/exposure';
import { useMutation, useQuery } from '../../lib/hooks';
import {
  confidenceMeta,
  findingLink,
  platformMeta,
  recommendedActionMeta,
  repoVisibilityMeta,
  severityMeta,
  SEVERITY_ORDER,
  toAllowlistPayload,
} from '../../lib/domain';
import {
  formatNumber,
  formatRelative,
  humanizeToken,
  parseAuthor,
  percentValue,
  shortBranch,
  shortCommit,
} from '../../lib/format';
import { exportRowsToCsv, timestampedName } from '../../lib/csv';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { Panel } from '../../ui/Panel';
import { Pagination } from '../../ui/Pagination';
import { SearchInput } from '../../ui/Field';
import { AppliedFilters, FacetRail, useFacetRail } from '../../ui/FacetRail';
import { RecordBar, ResultCount, ShowFiltersButton, WorkArea } from '../../ui/WorkArea';
import { SegmentedControl, Tabs } from '../../ui/Tabs';
import { OverflowMenu, RefreshButton, TableSettings, TableToolbar } from '../../ui/TableTools';
import { CellStack, DataGrid } from '../../ui/DataGrid';
import { Menu } from '../../ui/Menu';
import { Tag } from '../../ui/Tag';
import { MetricTile } from '../../ui/Stat';
import { useToast } from '../../ui/Toast';
import { ClearState, EmptyState, ErrorState } from '../../ui/States';
import { cn, TONE_FG } from '../../ui/cn';
import { GridSkeleton, StatStripSkeleton } from '../../ui/Skeleton';
import { DeepScanDrawer } from './DeepScanDrawer';
import { FindingDrawer } from './FindingDrawer';
import { describeExposureError, findingKey, groupByPush, summariseFindings, VERDICTS, verdictMeta } from './scannerState';

/**
 * Rows per page.
 *
 * `fetchFindings` reads every page of the live set (the API serves 500 at a
 * time), so paging on screen happens here, over the filtered and sorted array.
 * That is not a smaller request, it is a shorter page: a single repository can
 * carry hundreds of findings, and a browser asked to lay out 800 grid rows
 * drops frames on every sort and every keystroke in the search box.
 */
const DEFAULT_PAGE_SIZE = 25;

const VIEWS = [
  { value: 'findings', label: 'Credentials', icon: List },
  { value: 'pushes', label: 'By push', icon: GitCommitHorizontal },
];

/**
 * Credential exposure triage.
 *
 * The whole set is read (every page), so search, filtering, sorting and
 * grouping all happen client-side here. Two sets exist: the live findings,
 * and the ones the detection model scored as false positives, which the API
 * keeps out of the live set and serves separately. The writes are an
 * analyst's verdict on a finding and adding a finding to the allowlist.
 */
export default function FindingsPage() {
  const { notify } = useToast();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();
  /* `?set=flagged` shows the model-flagged set; it is a different record set,
     not a filter, so it lives in the URL and survives a reload. */
  const source = searchParams.get('set') === 'flagged' ? 'flagged' : 'live';
  const flaggedView = source === 'flagged';
  const query = useQuery((signal) => fetchFindings(signal, { flagged: flaggedView }), [flaggedView]);
  /* The count for the switch. Only the number is used. */
  const flaggedCount = useQuery((signal) => fetchFindings(signal, { flagged: true }), [], { enabled: !flaggedView });
  const dismissal = useMutation((input) => dismissFinding(input));
  const decision = useMutation((input) => decideFinding(input));
  /* Verdicts recorded in this session, applied over the loaded rows so the
     drawer and the grid change the moment the write succeeds. */
  const [verdicts, setVerdicts] = useState(() => new Map());

  const [platform, setPlatform] = useState('all');
  const [tier, setTier] = useState('');
  const [detector, setDetector] = useState('');
  const [repository, setRepository] = useState('');
  const [verdict, setVerdict] = useState('');
  /* Seeded from `?search=` so a link lands on the finding it names. Only the
     starting value: the box is local state from then on. */
  const [search, setSearch] = useState(() => searchParams.get('search') || '');
  const { railOpen, toggleRail } = useFacetRail();
  const [density, setDensity] = useState('comfortable');
  const [view, setView] = useState('findings');
  const [category, setCategory] = useState('');
  const [confidence, setConfidence] = useState('');
  const [visibility, setVisibility] = useState('');
  const [sort, setSort] = useState({ key: 'detected', direction: 'desc' });
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [selected, setSelected] = useState(null);
  /* `?deep-scan=open` opens the drawer on arrival (the Integrations catalog
     links here). Only the starting value, like `?search=`. */
  const [deepScanOpen, setDeepScanOpen] = useState(() => searchParams.get('deep-scan') === 'open');
  /* Locally dismissed ids, so a row leaves the list the moment the write
     succeeds rather than after a full refetch. */
  const [dismissed, setDismissed] = useState(() => new Set());

  /* Any change to what is being listed sends the reader back to page one.
     Without this, narrowing a 400-row list to 12 while on page 6 shows an
     empty grid and reads as "no results" - the single most common pagination
     bug there is. Wrapped rather than repeated at each call site so a filter
     added later cannot forget it. */
  const withReset = useCallback(
    (setter) => (value) => {
      setPage(1);
      setter(value);
    },
    [],
  );

  const allFindings = useMemo(
    () =>
      (query.data?.findings ?? [])
        .filter((finding) => !dismissed.has(findingKey(finding)))
        .map((finding) => {
          const recorded = verdicts.get(findingKey(finding));
          return recorded ? { ...finding, ...recorded } : finding;
        }),
    [query.data, dismissed, verdicts],
  );

  /* The drawer shows the row as it is now, including a verdict just made. */
  const selectedRow = useMemo(
    () => (selected ? allFindings.find((finding) => findingKey(finding) === findingKey(selected)) ?? selected : null),
    [selected, allFindings],
  );

  const switchSource = (next) => {
    const params = new URLSearchParams(searchParams);
    if (next === 'flagged') params.set('set', 'flagged');
    else params.delete('set');
    setSearchParams(params, { replace: true });
    setSelected(null);
    setPage(1);
  };

  const summary = useMemo(() => summariseFindings(allFindings), [allFindings]);

  const platformFiltered = useMemo(() => {
    if (platform === 'all') return allFindings;
    /* A finding recorded before platform tracking existed reports null and is
       always CodeCommit, so the CodeCommit filter is "not GitHub". */
    if (platform === 'github')
      return allFindings.filter((finding) => String(finding.platform).toLowerCase() === 'github');
    return allFindings.filter((finding) => String(finding.platform).toLowerCase() !== 'github');
  }, [allFindings, platform]);

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return platformFiltered.filter((finding) => {
      if (tier && String(finding.risk_tier).toUpperCase() !== tier) return false;
      if (detector && finding.detector !== detector) return false;
      if (repository && finding.repository !== repository) return false;
      if (category && String(finding.category || '') !== category) return false;
      if (confidence && String(finding.confidence || '').toLowerCase() !== confidence) return false;
      if (visibility && String(finding.repo_visibility || '').toLowerCase() !== visibility) return false;
      if (verdict && verdictMeta(finding.analyst_decision).key !== verdict) return false;
      if (!needle) return true;
      /* The enrichment is searchable too: a commit message is often the fastest
         way back to a finding somebody described in a ticket. */
      return [
        finding.repository,
        finding.file_path,
        finding.author,
        finding.detector,
        finding.branch,
        finding.commit_message,
        finding.committer,
        finding.line_preview,
        finding.category,
      ]
        .filter(Boolean)
        .some((field) => String(field).toLowerCase().includes(needle));
    });
  }, [platformFiltered, tier, detector, repository, category, confidence, visibility, verdict, search]);

  const sorted = useMemo(() => {
    const rows = [...filtered];
    const factor = sort.direction === 'asc' ? 1 : -1;
    rows.sort((a, b) => {
      if (sort.key === 'risk') {
        const delta = severityMeta(a.risk_tier).weight - severityMeta(b.risk_tier).weight;
        if (delta !== 0) return delta * factor;
        return (new Date(b.created_at) - new Date(a.created_at)) * -1;
      }
      if (sort.key === 'repository') {
        return String(a.repository || '').localeCompare(String(b.repository || '')) * factor;
      }
      return (new Date(a.created_at) - new Date(b.created_at)) * factor;
    });
    return rows;
  }, [filtered, sort]);

  const pushes = useMemo(() => {
    const groups = groupByPush(sorted);
    return groups.sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt));
  }, [sorted]);

  /* The two views page independently, because a page of 25 pushes is not a
     page of 25 findings. Whichever is on screen supplies the total. */
  const pagedTotal = view === 'pushes' ? pushes.length : sorted.length;
  /* Clamped rather than trusted. A dismiss on the last page, or a filter that
     shrinks the set, can leave `page` past the end - and a page past the end
     renders a blank grid that looks like a bug rather than an empty page. */
  const safePage = Math.min(page, Math.max(1, Math.ceil(pagedTotal / pageSize)));
  const pageStart = (safePage - 1) * pageSize;
  const pageRows = useMemo(
    () => sorted.slice(pageStart, pageStart + pageSize),
    [sorted, pageStart, pageSize],
  );
  const pagePushes = useMemo(
    () => pushes.slice(pageStart, pageStart + pageSize),
    [pushes, pageStart, pageSize],
  );

  /* Every facet count is computed from the live set the service returned, so
     the numbers and the rows can never disagree. */
  const repositoryOptions = useMemo(() => {
    const counts = new Map();
    for (const finding of platformFiltered) {
      if (!finding.repository) continue;
      counts.set(finding.repository, (counts.get(finding.repository) || 0) + 1);
    }
    return [...counts.entries()]
      .sort((a, b) => b[1] - a[1])
      .map(([key, count]) => ({ value: key, label: key, count, active: repository === key }));
  }, [platformFiltered, repository]);

  const categoryOptions = useMemo(
    () =>
      countBy(platformFiltered, (finding) => finding.category).map(([key, count]) => ({
        value: key,
        label: humanizeToken(key),
        count,
        active: category === key,
      })),
    [platformFiltered, category],
  );

  const confidenceOptions = useMemo(
    () =>
      countBy(platformFiltered, (finding) => String(finding.confidence || '').toLowerCase()).map(([key, count]) => ({
        value: key,
        label: confidenceMeta(key)?.short ?? humanizeToken(key),
        count,
        active: confidence === key,
      })),
    [platformFiltered, confidence],
  );

  const visibilityOptions = useMemo(
    () =>
      countBy(platformFiltered, (finding) => String(finding.repo_visibility || '').toLowerCase()).map(([key, count]) => ({
        value: key,
        label: repoVisibilityMeta(key)?.label ?? humanizeToken(key),
        count,
        active: visibility === key,
      })),
    [platformFiltered, visibility],
  );

  const verdictOptions = useMemo(
    () =>
      countBy(platformFiltered, (finding) => verdictMeta(finding.analyst_decision).key).map(([key, count]) => ({
        value: key,
        label: VERDICTS[key]?.label ?? humanizeToken(key),
        count,
        active: verdict === key,
      })),
    [platformFiltered, verdict],
  );

  const facetGroups = useMemo(
    () => [
      {
        key: 'tier',
        label: 'Risk tier',
        options: SEVERITY_ORDER.filter((value) => (summary.byTier[value] || 0) > 0).map((value) => ({
          value,
          label: severityMeta(value).label,
          count: summary.byTier[value],
          active: tier === value,
        })),
        onToggle: (value) => {
          setPage(1);
          setTier((current) => (current === value ? '' : value));
        },
      },
      {
        key: 'verdict',
        label: 'Analyst verdict',
        options: verdictOptions,
        onToggle: (value) => {
          setPage(1);
          setVerdict((current) => (current === value ? '' : value));
        },
        note: 'Recorded from the finding drawer. A verdict does not remove a finding - mark it as safe to move it to Accepted.',
      },
      {
        key: 'detector',
        label: 'Detector',
        options: summary.detectors.map((entry) => ({
          value: entry.key,
          label: humanizeToken(entry.key),
          count: entry.value,
          active: detector === entry.key,
        })),
        onToggle: (value) => {
          setPage(1);
          setDetector((current) => (current === value ? '' : value));
        },
      },
      {
        key: 'repository',
        label: 'Repository',
        options: repositoryOptions,
        onToggle: (value) => {
          setPage(1);
          setRepository((current) => (current === value ? '' : value));
        },
      },
      {
        key: 'visibility',
        label: 'Repository visibility',
        options: visibilityOptions,
        onToggle: (value) => {
          setPage(1);
          setVisibility((current) => (current === value ? '' : value));
        },
        note: 'Reported by GitHub only. A CodeCommit repository is private to its AWS account by definition, so the scanner has nothing to report.',
      },
      {
        key: 'category',
        label: 'Secret category',
        options: categoryOptions,
        onToggle: (value) => {
          setPage(1);
          setCategory((current) => (current === value ? '' : value));
        },
      },
      {
        key: 'confidence',
        label: 'Match confidence',
        options: confidenceOptions,
        onToggle: (value) => {
          setPage(1);
          setConfidence((current) => (current === value ? '' : value));
        },
        note: 'How certain the pattern match is. Separate from risk tier, which is how bad the secret would be if real.',
      },
    ],
    [summary, tier, detector, verdictOptions, repositoryOptions, visibilityOptions, categoryOptions, confidenceOptions],
  );

  const chips = useMemo(() => {
    const list = [];
    if (search.trim()) list.push({ key: 'search', label: 'Search', value: search.trim() });
    if (tier) list.push({ key: 'tier', label: 'Risk', value: severityMeta(tier).label });
    if (detector) list.push({ key: 'detector', label: 'Detector', value: humanizeToken(detector) });
    if (repository) list.push({ key: 'repository', label: 'Repo', value: repository });
    if (verdict) list.push({ key: 'verdict', label: 'Verdict', value: VERDICTS[verdict]?.label ?? verdict });
    if (visibility) {
      list.push({ key: 'visibility', label: 'Visibility', value: repoVisibilityMeta(visibility)?.label ?? visibility });
    }
    if (category) list.push({ key: 'category', label: 'Category', value: humanizeToken(category) });
    if (confidence) {
      list.push({ key: 'confidence', label: 'Confidence', value: confidenceMeta(confidence)?.short ?? confidence });
    }
    return list;
  }, [search, tier, detector, repository, verdict, visibility, category, confidence]);

  const removeChip = (key) => {
    if (key === 'search') setSearch('');
    if (key === 'tier') setTier('');
    if (key === 'detector') setDetector('');
    if (key === 'repository') setRepository('');
    if (key === 'verdict') setVerdict('');
    if (key === 'visibility') setVisibility('');
    if (key === 'category') setCategory('');
    if (key === 'confidence') setConfidence('');
  };

  const clearAll = () => {
    setSearch('');
    setTier('');
    setDetector('');
    setRepository('');
    setVerdict('');
    setVisibility('');
    setCategory('');
    setConfidence('');
  };

  const onExport = () => {
    exportRowsToCsv({
      filename: timestampedName(flaggedView ? 'code-exposure-model-flagged' : 'code-exposure-findings'),
      columns: [
        { header: 'Detector', value: (row) => row.detector },
        { header: 'Risk tier', value: (row) => row.risk_tier },
        { header: 'Recommended action', value: (row) => row.recommended_action },
        { header: 'Platform', value: (row) => row.platform || 'codecommit' },
        { header: 'Repository', value: (row) => row.repository },
        { header: 'Branch', value: (row) => row.branch },
        { header: 'Commit', value: (row) => row.commit_id },
        { header: 'File', value: (row) => row.file_path },
        { header: 'Line', value: (row) => row.line_number },
        { header: 'Masked value', value: (row) => row.redacted },
        { header: 'Author', value: (row) => row.author },
        { header: 'Detected (UTC)', value: (row) => row.created_at },
        /* The enrichment, written verbatim rather than formatted: a CSV is read
           by a script as often as by a person, and an empty cell is the honest
           rendering of a field this finding does not carry. */
        { header: 'Category', value: (row) => row.category || '' },
        { header: 'Match confidence', value: (row) => row.confidence || '' },
        { header: 'Line preview (masked)', value: (row) => row.line_preview || '' },
        { header: 'Committer', value: (row) => row.committer || '' },
        { header: 'Commit message', value: (row) => row.commit_message || '' },
        { header: 'Commit authored at (raw)', value: (row) => row.commit_authored_at || '' },
        { header: 'Files changed', value: (row) => row.files_changed ?? '' },
        { header: 'Additions', value: (row) => row.additions ?? '' },
        { header: 'Deletions', value: (row) => row.deletions ?? '' },
        { header: 'Repo visibility', value: (row) => row.repo_visibility || '' },
        { header: 'Repo stars', value: (row) => row.repo_stars ?? '' },
        { header: 'Repo forks', value: (row) => row.repo_forks ?? '' },
        { header: 'Repo last pushed', value: (row) => row.repo_pushed_at || '' },
        { header: 'Model decision', value: (row) => row.ml_decision || '' },
        { header: 'Model score', value: (row) => row.ml_score ?? '' },
        { header: 'Analyst verdict', value: (row) => row.analyst_decision || '' },
        { header: 'Verdict by', value: (row) => row.analyst_decided_by || '' },
        { header: 'Link', value: (row) => row.codecommit_uri || row.github_uri || '' },
      ],
      rows: sorted,
    });
    notify({
      variant: 'success',
      title: 'Exported current view',
      description: `${formatNumber(sorted.length)} findings written to CSV, with masked values only.`,
    });
  };

  const onDismiss = useCallback(
    async (finding, reason) => {
      const result = await dismissal.mutate(toAllowlistPayload(finding, reason));
      if (!result.ok) {
        notify({
          variant: 'error',
          title: 'Could not mark it safe',
          description: describeExposureError(result.error).message,
        });
        return;
      }
      setDismissed((current) => new Set(current).add(findingKey(finding)));
      setSelected(null);
      notify({
        variant: 'success',
        title: 'Marked as safe',
        description: `${finding.file_path} moved to Accepted. You can restore it from there.`,
        action: { label: 'View Accepted', onSelect: () => navigate('/exposure/dismissed') },
      });
    },
    [dismissal, notify, navigate],
  );

  const onDecide = useCallback(
    async (finding, value) => {
      if (!finding.finding_id) return;
      const result = await decision.mutate({ findingId: finding.finding_id, decision: value });
      if (!result.ok) {
        notify({
          variant: 'error',
          title: 'Verdict not saved',
          description:
            result.error?.status === 404
              ? 'That finding no longer exists. Refresh the list.'
              : describeExposureError(result.error).message,
        });
        return;
      }
      setVerdicts((current) =>
        new Map(current).set(findingKey(finding), {
          analyst_decision: value,
          analyst_decided_at: new Date().toISOString(),
          analyst_decided_by: user?.email || user?.name || 'you',
        }),
      );
      notify({
        variant: 'success',
        title: 'Verdict recorded',
        description: `Marked as ${verdictMeta(value).label.toLowerCase()}.`,
      });
    },
    [decision, notify, user],
  );

  if (query.isError && !query.data) {
    const detail = describeExposureError(query.error);
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title={flaggedView ? 'Model-flagged findings' : 'Exposed credentials'}
          lede="Credentials committed to connected repositories."
          actions={
            flaggedView ? (
              <Button variant="secondary" onClick={() => switchSource('live')}>
                Back to live findings
              </Button>
            ) : undefined
          }
        />
        <Panel>
          <ErrorState error={{ message: detail.message, code: query.error?.code }} title={detail.title} onRetry={query.refetch} />
        </Panel>
      </div>
    );
  }

  const loading = query.isLoading && !query.data;
  /* High, plus critical when the scanner raises it. A tile of its own for
     critical would mostly read zero, which looks like good news. */
  const highish = (summary.byTier.CRITICAL || 0) + (summary.byTier.HIGH || 0);

  const columns = [
    {
      key: 'finding',
      header: 'Secret',
      primary: true,
      width: '28%',
      cell: (row) => (
        <CellStack icon={FileWarning} title={humanizeToken(row.detector)} meta={`${row.file_path}:${row.line_number ?? '?'}`} mono />
      ),
    },
    {
      key: 'risk',
      header: 'Risk',
      width: '10%',
      sortable: true,
      cell: (row) => {
        const meta = severityMeta(row.risk_tier);
        return (
          <Tag tone={meta.tone} size="sm" dot>
            {meta.label}
          </Tag>
        );
      },
    },
    {
      key: 'repository',
      header: 'Repository',
      width: '20%',
      sortable: true,
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[12.5px] text-ink-2" title={row.repository}>
            {row.repository || '-'}
          </span>
          <span className="block truncate font-mono text-[11px] text-ink-3">
            {shortBranch(row.branch)} · {shortCommit(row.commit_id)}
          </span>
        </span>
      ),
    },
    {
      key: 'author',
      header: 'Author',
      width: '16%',
      priority: 'wide',
      cell: (row) => {
        const author = parseAuthor(row.author);
        return (
          <span className="block min-w-0">
            <span className="block truncate text-[12.5px] text-ink-2">{author.name}</span>
            {author.email && (
              <span className="block truncate text-[11px] text-ink-3">{author.email}</span>
            )}
          </span>
        );
      },
    },
    {
      key: 'action',
      header: 'Recommended',
      width: '13%',
      priority: 'wide',
      cell: (row) => {
        const meta = recommendedActionMeta(row.recommended_action);
        return (
          <span className={cn('text-[12px] font-medium', TONE_FG[meta.tone])}>{meta.label}</span>
        );
      },
    },
    {
      key: 'detected',
      header: 'Detected',
      width: '13%',
      sortable: true,
      cell: (row) => <span className="text-[12.5px] text-ink-2">{formatRelative(row.created_at)}</span>,
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={flaggedView ? 'Model-flagged findings' : 'Exposed credentials'}
        lede={
          flaggedView
            ? 'Matches the detection model scored as false positives. They are kept out of the live list - confirm the model, or mark one as a real secret.'
            : 'Secret values are masked. To fix a finding, revoke or rotate the secret where it was issued, then remove it from the code.'
        }
        tabs={
          <Tabs
            size="sm"
            value={platform}
            onChange={withReset(setPlatform)}
            tabs={[
              { value: 'all', label: 'All platforms', count: summary.total },
              { value: 'github', label: 'GitHub', count: summary.byPlatform.github },
              { value: 'codecommit', label: 'CodeCommit', count: summary.byPlatform.codecommit },
            ]}
          />
        }
        actions={
          <>
            {/* A deep scan is something you do to a repository already on this
                screen, so it opens here rather than living in the navigation
                as a peer of the findings it produces. */}
            {flaggedView ? (
              <Button variant="secondary" onClick={() => switchSource('live')}>
                Back to live findings
              </Button>
            ) : (
              <>
                <Button variant="secondary" icon={Radar} onClick={() => setDeepScanOpen(true)}>
                  Deep scan
                </Button>
                <Button variant="ghost" icon={Sparkles} onClick={() => switchSource('flagged')}>
                  Model-flagged
                  {Number.isFinite(flaggedCount.data?.count) && (
                    <span data-numeric="" className="ml-1 rounded-full bg-surface-3 px-1.5 text-[11px] font-semibold text-ink-2">
                      {formatNumber(flaggedCount.data.count)}
                    </span>
                  )}
                </Button>
              </>
            )}
            <Button as={Link} to="/exposure/dismissed" variant="ghost" icon={ShieldOff}>
              Accepted
            </Button>
          </>
        }
      />

      {loading ? (
        <StatStripSkeleton count={4} />
      ) : (
        <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4">
          <MetricTile
            label={flaggedView ? 'Model-flagged' : 'Live exposures'}
            value={summary.total}
            icon={flaggedView ? Sparkles : FileWarning}
            tone={flaggedView ? 'info' : summary.total > 0 ? 'high' : 'low'}
            caption={
              flaggedView
                ? 'Scored as false positives by the detection model'
                : 'Excludes findings marked safe and model-flagged false positives'
            }
          />
          <MetricTile
            label="High or critical"
            value={highish}
            tone={highish > 0 ? 'critical' : 'low'}
            caption={`${formatNumber(summary.byTier.MEDIUM || 0)} medium, ${formatNumber(summary.byTier.LOW || 0)} low`}
            meter={percentValue(highish, summary.total)}
            meterLabel="Share of live exposures"
          />
          <MetricTile
            label="Repositories affected"
            value={summary.repositoryCount}
            tone="info"
            caption="Distinct repositories across both platforms"
          />
          <MetricTile
            label="Detector types"
            value={summary.detectors.length}
            tone="medium"
            caption={
              summary.detectors[0]
                ? `Most common: ${humanizeToken(summary.detectors[0].key)}`
                : 'No detections'
            }
          />
        </div>
      )}

      <WorkArea
        railOpen={railOpen}
        rail={
          <FacetRail
            groups={facetGroups}
            appliedCount={chips.length}
            onClearAll={clearAll}
            onClose={toggleRail}
            mobileTitle="Filter exposures"
          />
        }
      >
      <Panel prominence="lead" flush className="animate-rise overflow-hidden">
        <RecordBar
          trailing={
            <TableToolbar>
              {/* View mode stays in the open: it changes which records are
                  listed, which is not a display preference. */}
              <SegmentedControl label="View mode" options={VIEWS} value={view} onChange={withReset(setView)} />
              {!railOpen && (
                <ShowFiltersButton onClick={toggleRail} appliedCount={chips.length} />
              )}
              <RefreshButton onRefresh={query.refetch} refreshing={query.isRefreshing} />
              <TableSettings density={density} onDensityChange={setDensity} />
              <OverflowMenu
                items={[
                  {
                    key: 'export',
                    label: 'Export this view',
                    hint: `CSV of all ${sorted.length} matching findings`,
                    onSelect: onExport,
                    disabled: sorted.length === 0,
                    disabledHint: 'Nothing to export - no findings match',
                  },
                ]}
              />
            </TableToolbar>
          }
        >
          <SearchInput
            size="sm"
            value={search}
            onChange={withReset(setSearch)}
            placeholder="Search repository, file, branch, detector or author…"
            className="w-full min-w-0 sm:max-w-sm"
          />
          <ResultCount
            shown={formatNumber(pagedTotal)}
            total={formatNumber(view === 'pushes' ? groupByPush(allFindings).length : summary.total)}
            unit={view === 'pushes' ? 'pushes' : flaggedView ? 'flagged findings' : 'exposed credentials'}
            filtered={chips.length > 0 || platform !== 'all'}
            loading={loading}
          />
        </RecordBar>

        <AppliedFilters filters={chips} onRemove={removeChip} onClearAll={clearAll} />

        {loading ? (
          <GridSkeleton columns={5} rows={10} />
        ) : view === 'findings' ? (
          <DataGrid
            caption="Exposed credentials"
            columns={columns}
            rows={pageRows}
            rowKey={findingKey}
            refreshing={query.isRefreshing}
            onRowClick={setSelected}
            density={density}
            sort={sort}
            onSortChange={withReset(setSort)}
            rowActions={(row) => {
              const link = findingLink(row);
              return (
                <Menu
                  label={`Actions for ${row.file_path}`}
                  items={[
                    link && {
                      key: 'open',
                      label: link.label,
                      icon: ExternalLink,
                      onSelect: () => window.open(link.href, '_blank', 'noopener,noreferrer'),
                    },
                    {
                      key: 'inspect',
                      label: 'Inspect finding',
                      icon: FileWarning,
                      onSelect: () => setSelected(row),
                    },
                  ]}
                />
              );
            }}
            emptyState={
              chips.length > 0 || platform !== 'all' ? (
                <EmptyState
                  icon={SearchX}
                  title="No exposures match this view"
                  description="Nothing in the live set matches the current platform, risk tier and search combination."
                  action={
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        clearAll();
                        setPlatform('all');
                      }}
                    >
                      Reset view
                    </Button>
                  }
                />
              ) : flaggedView ? (
                <EmptyState
                  icon={Sparkles}
                  title="Nothing flagged by the model"
                  description="The detection model has not scored any finding as a false positive, so everything the scanner found is in the live list."
                  action={
                    <Button variant="secondary" size="sm" onClick={() => switchSource('live')}>
                      Back to live findings
                    </Button>
                  }
                />
              ) : (
                <ClearState
                  title="No secrets exposed in code"
                  description="The scanner found nothing outstanding across the connected repositories. Anything previously reviewed lives in the Accepted view."
                  action={
                    <Button as={Link} to="/exposure/dismissed" variant="secondary" size="sm">
                      View accepted
                    </Button>
                  }
                />
              )
            }
          />
        ) : (
          <PushList pushes={pagePushes} onSelect={setSelected} />
        )}

        {/* Only once there is more than one page. A pager under a list of six
            is a control that can never be used. */}
        {!loading && pagedTotal > pageSize && (
          <Pagination
            page={safePage}
            pageSize={pageSize}
            total={pagedTotal}
            unit={view === 'pushes' ? 'pushes' : 'exposed credentials'}
            onPageChange={setPage}
            onPageSizeChange={(next) => {
              setPage(1);
              setPageSize(next);
            }}
          />
        )}
      </Panel>
      </WorkArea>

      <FindingDrawer
        finding={selectedRow}
        onClose={() => setSelected(null)}
        onDismiss={onDismiss}
        dismissing={dismissal.pending}
        onDecide={onDecide}
        deciding={decision.pending}
      />

      {/* The live set is passed in rather than fetched again: it is already
          loaded here, and a second identical request would only introduce a
          way for the two lists to disagree. */}
      <DeepScanDrawer
        open={deepScanOpen}
        onClose={() => setDeepScanOpen(false)}
        findings={allFindings}
      />
    </div>
  );
}

/**
 * Grouped view: one push can introduce several secrets at once, and the guide
 * recommends grouping by (repository, commit) so the blast radius of a single
 * commit reads as one item.
 */
function PushList({ pushes, onSelect }) {
  if (pushes.length === 0) {
    return (
      <EmptyState
        compact
        icon={GitCommitHorizontal}
        title="No pushes to group"
        description="No findings match the current filters, so there is nothing to group by commit."
      />
    );
  }

  return (
    <ul className="divide-y divide-line">
      {pushes.map((push) => {
        const author = parseAuthor(push.author);
        const worst = push.findings.reduce((acc, finding) => {
          const meta = severityMeta(finding.risk_tier);
          return meta.weight > acc.weight ? meta : acc;
        }, severityMeta(null));
        const platform = platformMeta(push.platform);

        return (
          <li key={push.key} className="px-4 py-3.5">
            <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <GitCommitHorizontal aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
                  <p className="min-w-0 truncate text-[13px] font-semibold text-ink" title={push.repository}>
                    {push.repository || '-'}
                  </p>
                  <span className="shrink-0 font-mono text-[11.5px] text-ink-3">
                    {shortCommit(push.commitId)}
                  </span>
                </div>
                <p className="mt-1 truncate text-[11.5px] text-ink-3">
                  {shortBranch(push.branch)} · {author.name} · {formatRelative(push.createdAt)}
                </p>
              </div>

              <div className="flex shrink-0 items-center gap-2">
                <Tag tone={platform.tone} size="sm">
                  {platform.label}
                </Tag>
                <Tag tone={worst.tone} size="sm" dot>
                  {worst.label}
                </Tag>
                <span
                  data-numeric=""
                  className="rounded-full bg-surface-3 px-2 py-0.5 text-[11.5px] font-semibold text-ink-2"
                >
                  {formatNumber(push.findings.length)} {push.findings.length === 1 ? 'secret' : 'secrets'}
                </span>
              </div>
            </div>

            <PushFindings findings={push.findings} onSelect={onSelect} />
          </li>
        );
      })}
    </ul>
  );
}

/* A push can carry dozens of secrets (a committed .env); the first few are
   listed and the rest sit behind "+N more" so one push never fills the page. */
const PUSH_PREVIEW = 5;

function PushFindings({ findings, onSelect }) {
  const [expanded, setExpanded] = useState(false);
  const hidden = findings.length - PUSH_PREVIEW;
  const shown = expanded || hidden <= 1 ? findings : findings.slice(0, PUSH_PREVIEW);
  return (
    <>
      <ul className="mt-2.5 flex flex-col gap-1">
        {shown.map((finding) => {
          const meta = severityMeta(finding.risk_tier);
          return (
            <li key={findingKey(finding)}>
              <button
                type="button"
                onClick={() => onSelect(finding)}
                className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-surface-2"
              >
                <span
                  aria-hidden="true"
                  className="size-1.5 shrink-0 rounded-full"
                  style={{ background: `var(--t-${meta.tone})` }}
                />
                <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink-2">
                  {finding.file_path}:{finding.line_number ?? '?'}
                </span>
                <span className="shrink-0 text-[11.5px] text-ink-3">
                  {humanizeToken(finding.detector)}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      {hidden > 1 && (
        <button
          type="button"
          onClick={() => setExpanded((value) => !value)}
          className="mt-1 px-2 text-[11.5px] font-medium text-brand hover:underline"
        >
          {expanded ? 'Show fewer' : `+${formatNumber(hidden)} more`}
        </button>
      )}
    </>
  );
}

/**
 * Facet counts read off the live set, the same way the repository facet does.
 *
 * The enrichment is optional per finding, so a hard-coded list of categories
 * or confidence levels would offer options that match nothing. Module level
 * rather than inline, so the memos that use it have stable dependencies.
 */
function countBy(findings, read) {
  const counts = new Map();
  for (const finding of findings) {
    const value = read(finding);
    if (!value) continue;
    counts.set(value, (counts.get(value) || 0) + 1);
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]);
}
