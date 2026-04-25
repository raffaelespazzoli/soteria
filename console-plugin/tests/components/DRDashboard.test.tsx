import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRDashboard from '../../src/components/DRDashboard/DRDashboard';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => (
    <title>{children}</title>
  ),
  ListPageHeader: ({ title }: { title: string }) => <h1>{title}</h1>,
}));

describe('DRDashboard', () => {
  it('renders the dashboard heading', () => {
    render(<DRDashboard />);
    expect(
      screen.getByRole('heading', { name: /disaster recovery/i }),
    ).toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<DRDashboard />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
