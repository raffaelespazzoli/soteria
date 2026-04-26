import {
  EmptyState,
  EmptyStateBody,
  Button,
} from '@patternfly/react-core';
import { CubesIcon } from '@patternfly/react-icons';

export function DashboardEmptyState() {
  return (
    <EmptyState
      titleText="No DR Plans configured"
      icon={CubesIcon}
      headingLevel="h4"
    >
      <EmptyStateBody>
        Create your first DR plan by labeling VMs with{' '}
        <code>app.kubernetes.io/part-of=&lt;app-name&gt;</code> and{' '}
        <code>soteria.io/wave=&lt;number&gt;</code>.
      </EmptyStateBody>
      <Button
        variant="link"
        component="a"
        href="https://github.com/soteria-project/soteria/blob/main/docs/getting-started.md"
        target="_blank"
        rel="noopener noreferrer"
      >
        View documentation
      </Button>
    </EmptyState>
  );
}
