import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

const ThemeContext = createContext(null);
const STORAGE_KEY = 'dna.theme';

function systemTheme() {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

/**
 * Appearance: follows the operating system until the operator picks a theme
 * with the top-bar toggle, and remembers that choice from then on. `theme` is
 * the resolved value the tokens use; `preference` is what was chosen
 * ('system' until the first press).
 */
export function ThemeProvider({ children }) {
  const [preference, setPreference] = useState(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system';
    } catch {
      return 'system';
    }
  });
  const [resolved, setResolved] = useState(() =>
    preference === 'system' ? systemTheme() : preference,
  );

  useEffect(() => {
    if (preference !== 'system') {
      setResolved(preference);
      return undefined;
    }
    const list = window.matchMedia('(prefers-color-scheme: dark)');
    const apply = () => setResolved(list.matches ? 'dark' : 'light');
    apply();
    list.addEventListener('change', apply);
    return () => list.removeEventListener('change', apply);
  }, [preference]);

  useEffect(() => {
    document.documentElement.dataset.theme = resolved;
  }, [resolved]);

  const choose = useCallback((next) => {
    setPreference(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* the in-memory preference still applies */
    }
  }, []);

  const value = useMemo(
    () => ({ theme: resolved, preference, setPreference: choose }),
    [resolved, preference, choose],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useThemeMode() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error('useThemeMode must be used inside a ThemeProvider');
  return context;
}
