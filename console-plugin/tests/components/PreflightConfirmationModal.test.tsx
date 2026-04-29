import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { axe, toHaveNoViolations } from 'jest-axe';
import { PreflightConfirmationModal } from '../../src/components/DRPlanDetail/PreflightConfirmationModal';
import { PreflightData } from '../../src/hooks/usePreflightData';

expect.extend(toHaveNoViolations);

const mockFailoverData: PreflightData = {
  vmCount: 12,
  waveCount: 3,
  estimatedRTO: '~18 min based on last execution',
  capacityAssessment: 'sufficient',
  actionSummary: 'Force-promote volumes on dc2-prod, start VMs wave by wave',
  primarySite: 'dc1-prod',
  secondarySite: 'dc2-prod',
  activeSite: 'dc1-prod',
};

const mockPlannedMigrationData: PreflightData = {
  ...mockFailoverData,
  actionSummary:
    'Step 0: Stop VMs on dc1-prod → wait for final replication sync → promote volumes on dc2-prod → start VMs wave by wave',
};

const mockReprotectData: PreflightData = {
  ...mockFailoverData,
  actionSummary:
    'Demote volumes on old active site, initiate replication resync, monitor until healthy',
};

const mockFailbackData: PreflightData = {
  ...mockFailoverData,
  actionSummary:
    'Step 0: Stop VMs on dc1-prod → wait for final replication sync → promote volumes on dc1-prod → start VMs wave by wave',
};

const mockRestoreData: PreflightData = {
  ...mockReprotectData,
};

const defaultProps = {
  isOpen: true,
  onClose: jest.fn(),
  onConfirm: jest.fn(),
  action: 'failover',
  planName: 'erp-full-stack',
  preflightData: mockFailoverData,
  isCreating: false,
};

describe('PreflightConfirmationModal', () => {
  beforeEach(() => {
    (defaultProps.onClose as jest.Mock).mockClear();
    (defaultProps.onConfirm as jest.Mock).mockClear();
  });

  describe('rendering for each action type', () => {
    it('renders modal with pre-flight summary for Failover', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(screen.getByText(/Confirm Failover: erp-full-stack/)).toBeInTheDocument();
      expect(screen.getByText(/12 VMs across 3 waves/)).toBeInTheDocument();
      expect(screen.getByText(/~18 min based on last execution/)).toBeInTheDocument();
    });

    it('renders modal for Planned Migration', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="planned_migration"
          preflightData={mockPlannedMigrationData}
        />,
      );
      expect(screen.getByText(/Confirm Planned Migration: erp-full-stack/)).toBeInTheDocument();
    });

    it('renders modal for Reprotect', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="reprotect"
          preflightData={mockReprotectData}
        />,
      );
      expect(screen.getByText(/Confirm Reprotect: erp-full-stack/)).toBeInTheDocument();
    });

    it('renders modal for Failback', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="failback"
          preflightData={mockFailbackData}
        />,
      );
      expect(screen.getByText(/Confirm Failback: erp-full-stack/)).toBeInTheDocument();
    });

    it('renders modal for Restore', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="restore"
          preflightData={mockRestoreData}
        />,
      );
      expect(screen.getByText(/Confirm Restore: erp-full-stack/)).toBeInTheDocument();
    });
  });

  describe('confirmation keyword validation', () => {
    it('Confirm button disabled until keyword matches exactly', async () => {
      const user = userEvent.setup();
      render(<PreflightConfirmationModal {...defaultProps} />);
      const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
      expect(confirmBtn).toBeDisabled();

      const input = screen.getByLabelText(/type FAILOVER to confirm/i);
      await user.type(input, 'FAIL');
      expect(confirmBtn).toBeDisabled();

      await user.type(input, 'OVER');
      expect(confirmBtn).toBeEnabled();
    });

    it('Confirm button enabled after typing correct keyword for Planned Migration', async () => {
      const user = userEvent.setup();
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="planned_migration"
          preflightData={mockPlannedMigrationData}
        />,
      );
      const confirmBtn = screen.getByRole('button', { name: /confirm planned migration/i });
      expect(confirmBtn).toBeDisabled();

      const input = screen.getByLabelText(/type MIGRATE to confirm/i);
      await user.type(input, 'MIGRATE');
      expect(confirmBtn).toBeEnabled();
    });

    it('Confirm button requires case-sensitive keyword', async () => {
      const user = userEvent.setup();
      render(<PreflightConfirmationModal {...defaultProps} />);
      const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
      const input = screen.getByLabelText(/type FAILOVER to confirm/i);
      await user.type(input, 'failover');
      expect(confirmBtn).toBeDisabled();
    });

    it('Confirm button stays disabled during creation', async () => {
      const user = userEvent.setup();
      render(<PreflightConfirmationModal {...defaultProps} isCreating />);
      const input = screen.getByLabelText(/type FAILOVER to confirm/i);
      await user.type(input, 'FAILOVER');
      const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
      expect(confirmBtn).toBeDisabled();
    });
  });

  describe('button variants', () => {
    it('Failover uses danger variant for Confirm button', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
      expect(confirmBtn).toHaveClass('pf-m-danger');
    });

    it('Planned Migration uses primary variant for Confirm button', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="planned_migration"
          preflightData={mockPlannedMigrationData}
        />,
      );
      const confirmBtn = screen.getByRole('button', { name: /confirm planned migration/i });
      expect(confirmBtn).toHaveClass('pf-m-primary');
    });

    it('Reprotect uses primary variant for Confirm button', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="reprotect"
          preflightData={mockReprotectData}
        />,
      );
      const confirmBtn = screen.getByRole('button', { name: /confirm reprotect/i });
      expect(confirmBtn).toHaveClass('pf-m-primary');
    });

    it('Failback uses primary variant for Confirm button', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="failback"
          preflightData={mockFailbackData}
        />,
      );
      const confirmBtn = screen.getByRole('button', { name: /confirm failback/i });
      expect(confirmBtn).toHaveClass('pf-m-primary');
    });

    it('Restore uses primary variant for Confirm button', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="restore"
          preflightData={mockRestoreData}
        />,
      );
      const confirmBtn = screen.getByRole('button', { name: /confirm restore/i });
      expect(confirmBtn).toHaveClass('pf-m-primary');
    });
  });

  describe('cancel and close behavior', () => {
    it('Cancel closes modal with no side effects', async () => {
      const onClose = jest.fn();
      const onConfirm = jest.fn();
      const user = userEvent.setup();
      render(
        <PreflightConfirmationModal {...defaultProps} onClose={onClose} onConfirm={onConfirm} />,
      );
      await user.click(screen.getByRole('button', { name: /cancel/i }));
      expect(onClose).toHaveBeenCalled();
      expect(onConfirm).not.toHaveBeenCalled();
    });

    it('Escape closes modal with no side effects', async () => {
      const onClose = jest.fn();
      const onConfirm = jest.fn();
      const user = userEvent.setup();
      render(
        <PreflightConfirmationModal {...defaultProps} onClose={onClose} onConfirm={onConfirm} />,
      );
      await user.keyboard('{Escape}');
      expect(onClose).toHaveBeenCalled();
      expect(onConfirm).not.toHaveBeenCalled();
    });
  });

  describe('confirm action', () => {
    it('calls onConfirm when keyword is correct and Confirm is clicked', async () => {
      const onConfirm = jest.fn();
      const user = userEvent.setup();
      render(<PreflightConfirmationModal {...defaultProps} onConfirm={onConfirm} />);
      const input = screen.getByLabelText(/type FAILOVER to confirm/i);
      await user.type(input, 'FAILOVER');
      await user.click(screen.getByRole('button', { name: /confirm failover/i }));
      expect(onConfirm).toHaveBeenCalledTimes(1);
    });
  });

  describe('pre-flight content', () => {
    it('shows VM count and wave count', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(screen.getByText(/12 VMs across 3 waves/)).toBeInTheDocument();
    });

    it('shows estimated duration', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(screen.getByText(/~18 min based on last execution/)).toBeInTheDocument();
    });

    it('shows capacity assessment', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(screen.getByText(/Sufficient/)).toBeInTheDocument();
    });

    it('shows action summary', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(
        screen.getByText('Force-promote volumes on dc2-prod, start VMs wave by wave'),
      ).toBeInTheDocument();
    });
  });

  describe('error display', () => {
    it('displays inline error when creation fails', () => {
      render(
        <PreflightConfirmationModal
          {...defaultProps}
          error="concurrent execution already active"
        />,
      );
      expect(screen.getByText('Failed to create execution')).toBeInTheDocument();
      expect(screen.getByText('concurrent execution already active')).toBeInTheDocument();
    });

    it('does not display error section when no error', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(screen.queryByText('Failed to create execution')).not.toBeInTheDocument();
    });
  });

  describe('accessibility', () => {
    it('passes jest-axe for Failover variant', async () => {
      const { container } = render(<PreflightConfirmationModal {...defaultProps} />);
      expect(await axe(container)).toHaveNoViolations();
    });

    it('passes jest-axe for Planned Migration variant', async () => {
      const { container } = render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="planned_migration"
          preflightData={mockPlannedMigrationData}
        />,
      );
      expect(await axe(container)).toHaveNoViolations();
    });

    it('passes jest-axe for Reprotect variant', async () => {
      const { container } = render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="reprotect"
          preflightData={mockReprotectData}
        />,
      );
      expect(await axe(container)).toHaveNoViolations();
    });

    it('passes jest-axe for Failback variant', async () => {
      const { container } = render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="failback"
          preflightData={mockFailbackData}
        />,
      );
      expect(await axe(container)).toHaveNoViolations();
    });

    it('passes jest-axe for Restore variant', async () => {
      const { container } = render(
        <PreflightConfirmationModal
          {...defaultProps}
          action="restore"
          preflightData={mockRestoreData}
        />,
      );
      expect(await axe(container)).toHaveNoViolations();
    });

    it('passes jest-axe with error displayed', async () => {
      const { container } = render(
        <PreflightConfirmationModal {...defaultProps} error="some error" />,
      );
      expect(await axe(container)).toHaveNoViolations();
    });

    it('confirmation input has accessible label', () => {
      render(<PreflightConfirmationModal {...defaultProps} />);
      expect(screen.getByLabelText(/type FAILOVER to confirm/i)).toBeInTheDocument();
    });
  });

  describe('loading state', () => {
    it('shows loading spinner on Confirm button during creation', () => {
      render(<PreflightConfirmationModal {...defaultProps} isCreating />);
      const confirmBtn = screen.getByRole('button', { name: /confirm failover/i });
      expect(confirmBtn).toBeDisabled();
    });
  });
});
