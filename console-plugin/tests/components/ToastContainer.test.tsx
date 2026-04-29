import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { axe, toHaveNoViolations } from 'jest-axe';
import ToastContainer from '../../src/components/shared/ToastContainer';
import { Toast } from '../../src/notifications/toastStore';

expect.extend(toHaveNoViolations);

const mockRemoveToast = jest.fn();
const mockPush = jest.fn();

let mockToasts: Toast[] = [];

jest.mock('../../src/hooks/useToastNotifications', () => ({
  useToastNotifications: () => ({
    toasts: mockToasts,
    removeToast: mockRemoveToast,
  }),
}));

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useHistory: () => ({ push: mockPush }),
}));

beforeEach(() => {
  mockRemoveToast.mockClear();
  mockPush.mockClear();
});

describe('ToastContainer', () => {
  it('renders AlertGroup with isToast', () => {
    mockToasts = [
      {
        id: '1',
        variant: 'success',
        title: 'Failover completed: 12 VMs in 17m',
        persistent: false,
        timeout: 15000,
        linkTo: '/disaster-recovery/executions/test-1',
        linkText: 'View execution',
      },
    ];
    render(<ToastContainer />);
    expect(screen.getByText('Failover completed: 12 VMs in 17m')).toBeInTheDocument();
  });

  it('renders correct Alert variants', () => {
    mockToasts = [
      { id: '1', variant: 'info', title: 'Info toast', persistent: false, timeout: 8000 },
      { id: '2', variant: 'danger', title: 'Danger toast', persistent: true, timeout: 0 },
    ];
    render(<ToastContainer />);
    expect(screen.getByText('Info toast')).toBeInTheDocument();
    expect(screen.getByText('Danger toast')).toBeInTheDocument();
  });

  it('dismiss calls removeToast', async () => {
    const user = userEvent.setup();
    mockToasts = [
      { id: 'dismiss-me', variant: 'info', title: 'Dismiss this', persistent: false, timeout: 8000 },
    ];
    render(<ToastContainer />);

    const closeButton = screen.getByLabelText('Close Info alert: alert: Dismiss this');
    await user.click(closeButton);
    expect(mockRemoveToast).toHaveBeenCalledWith('dismiss-me');
  });

  it('action link navigates correctly', async () => {
    const user = userEvent.setup();
    mockToasts = [
      {
        id: 'nav-toast',
        variant: 'success',
        title: 'Click me',
        persistent: false,
        timeout: 15000,
        linkTo: '/disaster-recovery/executions/test-nav',
        linkText: 'View execution',
      },
    ];
    render(<ToastContainer />);

    const link = screen.getByText('View execution');
    await user.click(link);
    expect(mockPush).toHaveBeenCalledWith('/disaster-recovery/executions/test-nav');
    expect(mockRemoveToast).toHaveBeenCalledWith('nav-toast');
  });

  it('shows max 4 toasts even if store has more', () => {
    mockToasts = Array.from({ length: 6 }, (_, i) => ({
      id: `toast-${i}`,
      variant: 'info' as const,
      title: `Toast ${i}`,
      persistent: false,
      timeout: 8000,
    }));
    render(<ToastContainer />);
    expect(screen.getByText('Toast 0')).toBeInTheDocument();
    expect(screen.getByText('Toast 3')).toBeInTheDocument();
    expect(screen.queryByText('Toast 4')).not.toBeInTheDocument();
  });

  it('renders empty when no toasts', () => {
    mockToasts = [];
    const { container } = render(<ToastContainer />);
    expect(container.querySelector('.pf-v6-c-alert')).not.toBeInTheDocument();
  });

  it('renders default link text when linkText is undefined', () => {
    mockToasts = [
      {
        id: 'default-link',
        variant: 'info',
        title: 'Default link test',
        persistent: false,
        timeout: 8000,
        linkTo: '/some/path',
      },
    ];
    render(<ToastContainer />);
    expect(screen.getByText('View details')).toBeInTheDocument();
  });

  it('passes jest-axe (with toasts)', async () => {
    mockToasts = [
      {
        id: 'axe-1',
        variant: 'success',
        title: 'Success toast',
        persistent: false,
        timeout: 15000,
        linkTo: '/disaster-recovery/executions/t1',
        linkText: 'View execution',
      },
      {
        id: 'axe-2',
        variant: 'warning',
        title: 'Warning toast',
        persistent: true,
        timeout: 0,
      },
    ];
    const { container } = render(<ToastContainer />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe (empty)', async () => {
    mockToasts = [];
    const { container } = render(<ToastContainer />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
