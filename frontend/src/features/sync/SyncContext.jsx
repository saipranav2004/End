import { createContext, useCallback, useContext, useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../../app/AuthContext';
import { useAccess } from '../../app/useAccess';
import { useToast } from '../../ui/Toast';
import * as engine from '../../lib/sync/engine';
import { SyncPanel } from './SyncPanel';

const SyncContext = createContext(null);

/**
 * The scan, as the rest of the console sees it: the run's state, a way to
 * start or follow it, and the panel that shows it. Mounted once inside the
 * signed-in shell, so a run continues while the user moves between screens.
 */
export function SyncProvider({ children }) {
  const { user } = useAuth();
  const { can } = useAccess();
  const { notify } = useToast();
  const navigate = useNavigate();
  const [panelOpen, setPanelOpen] = useState(false);
  const snapshot = useSyncExternalStore(engine.subscribe, engine.getState);

  const owner = user?.username || user?.name || '';
  useEffect(() => {
    if (!owner) return undefined;
    engine.init(owner);
    return () => engine.stop();
  }, [owner]);

  /* Told once, in words, when a run ends: the panel may be closed by then. */
  useEffect(
    () =>
      engine.onEvent(({ type, run }) => {
        if (type === 'done') {
          const d = run.discovery;
          notify({
            variant: 'success',
            title: 'Scan finished',
            description: [
              Number.isFinite(d.changes) ? `${d.changes} ${d.changes === 1 ? 'change' : 'changes'} since the last scan` : null,
              run.posture?.state === 'skipped' ? 'Posture scores were not refreshed.' : null,
            ]
              .filter(Boolean)
              .join(' · ') || 'Your data is up to date.',
            action: { label: 'See what changed', onSelect: () => navigate('/scans/latest/changes') },
            duration: 12000,
          });
        } else if (type === 'failed') {
          notify({ variant: 'error', title: "The scan didn't finish", description: run.error, action: { label: 'Open', onSelect: () => setPanelOpen(true) }, duration: 12000 });
        }
      }),
    [notify, navigate],
  );

  const start = useCallback(async () => {
    setPanelOpen(true);
    await engine.start();
  }, []);

  const value = useMemo(
    () => ({
      ...snapshot,
      canScan: can('scan.run'),
      active: engine.isRunActive(),
      start,
      openPanel: () => setPanelOpen(true),
      closePanel: () => setPanelOpen(false),
      panelOpen,
    }),
    [snapshot, can, start, panelOpen],
  );

  return (
    <SyncContext.Provider value={value}>
      {children}
      <SyncPanel open={panelOpen} onClose={() => setPanelOpen(false)} />
    </SyncContext.Provider>
  );
}

export function useSync() {
  const value = useContext(SyncContext);
  if (!value) throw new Error('useSync must be used inside SyncProvider');
  return value;
}
