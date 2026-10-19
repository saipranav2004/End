import { Component } from 'react';
import { ErrorState } from '../ui/States';

/**
 * Keeps one broken screen from blanking the whole console. The sidebar, top
 * bar and search stay usable; the screen offers a retry. AppShell keys it by
 * path, so moving to another screen starts clean.
 */
export class ScreenErrorBoundary extends Component {
  state = { error: null };

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error('Screen failed to render', error, info?.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <ErrorState
        title="This screen could not be shown"
        error={{ message: 'Something on this screen failed. The rest of the console still works - try again, or open another screen from the sidebar.' }}
        onRetry={() => this.setState({ error: null })}
      />
    );
  }
}
