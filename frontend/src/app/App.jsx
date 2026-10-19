import { lazy, Suspense } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { AuthProvider, useAuth } from './AuthContext';
import { homeFor } from '../lib/roles';
import { ThemeProvider } from './ThemeContext';
import { WorkspaceProvider } from './WorkspaceContext';
import { ConnectionGate } from './ConnectionGate';
import { RequireAuth } from './RequireAuth';
import { RequireFeature } from './RequireFeature';
import { ToastProvider } from '../ui/Toast';
import { RequirePermission } from './RequirePermission';
import { RouteFallback } from '../shell/RouteFallback';
import LoginPage from '../features/auth/LoginPage';

/* Routes are split per screen. The charting and graph libraries only ship
   with the screens that draw them, so first paint after sign-in stays small. */
const OverviewPage = lazy(() => import('../features/overview/OverviewPage'));
const PosturePage = lazy(() => import('../features/posture/PosturePage'));
const PostureIdentityPage = lazy(() => import('../features/posture/PostureIdentityPage'));
const ScanChangesPage = lazy(() => import('../features/changes/ScanChangesPage'));
const IdentityPage = lazy(() => import('../features/identities/IdentityPage'));
const IdentitiesPage = lazy(() => import('../features/identities/IdentitiesPage'));
const CredentialsPage = lazy(() => import('../features/credentials/CredentialsPage'));
const FindingsPage = lazy(() => import('../features/exposure/FindingsPage'));
const DismissedPage = lazy(() => import('../features/exposure/DismissedPage'));
const ActivityPage = lazy(() => import('../features/activity/ActivityPage'));
const AccessGraphPage = lazy(() => import('../features/access/AccessGraphPage'));
const AccessDetailPage = lazy(() => import('../features/access/AccessDetailPage'));
const GenomePage = lazy(() => import('../features/genome/GenomePage'));
const GenomeDetailPage = lazy(() => import('../features/genome/GenomeDetailPage'));
const IntegrationsPage = lazy(() => import('../features/integrations/IntegrationsPage'));
const NotFoundPage = lazy(() => import('../features/NotFoundPage'));

function Screen({ children }) {
  return <Suspense fallback={<RouteFallback />}>{children}</Suspense>;
}

export default function App() {
  return (
    <ThemeProvider>
      <ToastProvider>
        <BrowserRouter>
          <AuthProvider>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route
                element={
                  <RequireAuth>
                    <WorkspaceProvider>
                        <ConnectionGate />
                    </WorkspaceProvider>
                  </RequireAuth>
                }
              >
                <Route index element={<HomeRedirect />} />
                <Route path="/overview" element={<Screen><OverviewPage /></Screen>} />
                <Route path="/posture" element={<Screen><PosturePage /></Screen>} />
                {/* Posture detail is the Posture tab of the identity page;
                    old links still land on it. */}
                <Route path="/posture/:id" element={<Screen><PostureIdentityPage /></Screen>} />
                <Route path="/scans/:scanId/changes" element={<Screen><ScanChangesPage /></Screen>} />
                <Route path="/identities" element={<Screen><IdentitiesPage /></Screen>} />
                <Route path="/identities/:id" element={<Screen><IdentityPage /></Screen>} />
                <Route path="/credentials" element={<Screen><CredentialsPage /></Screen>} />
                <Route
                  path="/exposure"
                  element={
                    <Screen>
                      <RequirePermission permission="exposure.view" screen="Exposed credentials" title="Exposed credentials are not part of your role">
                        <RequireFeature feature="exposure" screen="Exposed credentials">
                          <FindingsPage />
                        </RequireFeature>
                      </RequirePermission>
                    </Screen>
                  }
                />
                <Route
                  path="/exposure/dismissed"
                  element={
                    <Screen>
                      <RequirePermission permission="exposure.view" screen="Accepted" title="Exposed credentials are not part of your role">
                        <RequireFeature feature="exposure" screen="Accepted">
                          <DismissedPage />
                        </RequireFeature>
                      </RequirePermission>
                    </Screen>
                  }
                />
                <Route path="/activity" element={<Screen><ActivityPage /></Screen>} />
                <Route path="/access-graph" element={<Screen><AccessGraphPage /></Screen>} />
                <Route path="/access-graph/:id" element={<Screen><AccessDetailPage /></Screen>} />
                <Route path="/genome" element={<Screen><GenomePage /></Screen>} />
                <Route path="/genome/:id" element={<Screen><GenomeDetailPage /></Screen>} />
                <Route path="/integrations" element={<Screen><IntegrationsPage /></Screen>} />
                <Route path="*" element={<Screen><NotFoundPage /></Screen>} />
              </Route>
            </Routes>
          </AuthProvider>
        </BrowserRouter>
      </ToastProvider>
    </ThemeProvider>
  );
}

/* The signed-in role's landing screen; see `homeFor`. */
function HomeRedirect() {
  const { user } = useAuth();
  return <Navigate to={homeFor(user?.role)} replace />;
}
