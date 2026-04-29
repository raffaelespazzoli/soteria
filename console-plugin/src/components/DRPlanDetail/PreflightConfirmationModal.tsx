import { useState } from 'react';
import {
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Button,
  TextInput,
  FormGroup,
  Alert,
} from '@patternfly/react-core';
import { PreflightData } from '../../hooks/usePreflightData';
import { ACTION_CONFIG, resolveActionKey } from '../../utils/drPlanActions';

interface PreflightConfirmationModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  action: string;
  planName: string;
  preflightData: PreflightData;
  isCreating: boolean;
  error?: string;
}

export function PreflightConfirmationModal({
  isOpen,
  onClose,
  onConfirm,
  action,
  planName,
  preflightData,
  isCreating,
  error,
}: PreflightConfirmationModalProps) {
  const [keywordInput, setKeywordInput] = useState('');

  const actionKey = resolveActionKey(action);
  const config = ACTION_CONFIG[actionKey];
  if (!config) return null;

  const { label, keyword, confirmVariant } = config;
  const isConfirmEnabled = keywordInput === keyword && !isCreating;

  return (
    <Modal variant={ModalVariant.large} isOpen={isOpen} onClose={onClose} aria-labelledby="preflight-modal-title">
      <ModalHeader title={`Confirm ${label}: ${planName}`} labelId="preflight-modal-title" />
      <ModalBody>
        <div
          style={{
            fontSize: 'var(--pf-v5-global--FontSize--xl)',
            fontWeight: 600,
            marginBottom: 'var(--pf-v5-global--spacer--sm)',
          }}
        >
          {preflightData.vmCount} VMs across {preflightData.waveCount} waves
        </div>

        <div style={{ marginBottom: 'var(--pf-v5-global--spacer--sm)' }}>
          Estimated duration: {preflightData.estimatedRTO}
        </div>

        <div style={{ marginBottom: 'var(--pf-v5-global--spacer--lg)' }}>
          DR site capacity:{' '}
          {preflightData.capacityAssessment.charAt(0).toUpperCase() +
            preflightData.capacityAssessment.slice(1)}
        </div>

        <hr
          style={{
            border: 'none',
            borderTop: '1px solid var(--pf-v5-global--BorderColor--100)',
            margin: 'var(--pf-v5-global--spacer--md) 0',
          }}
        />

        <div style={{ marginBottom: 'var(--pf-v5-global--spacer--lg)' }}>
          <div style={{ fontWeight: 'bold', marginBottom: 'var(--pf-v5-global--spacer--xs)' }}>
            Summary of actions:
          </div>
          <div>{preflightData.actionSummary}</div>
        </div>

        <hr
          style={{
            border: 'none',
            borderTop: '1px solid var(--pf-v5-global--BorderColor--100)',
            margin: 'var(--pf-v5-global--spacer--md) 0',
          }}
        />

        <FormGroup label={`Type ${keyword} to confirm`} fieldId="preflight-keyword">
          <TextInput
            id="preflight-keyword"
            value={keywordInput}
            onChange={(_event, value) => setKeywordInput(value)}
            style={{
              fontFamily: 'var(--pf-v5-global--FontFamily--monospace)',
              fontSize: 'var(--pf-v5-global--FontSize--lg)',
            }}
            aria-label={`Type ${keyword} to confirm`}
          />
        </FormGroup>

        {error && (
          <Alert
            variant="danger"
            isInline
            title="Failed to create execution"
            style={{ marginTop: 'var(--pf-v5-global--spacer--md)' }}
          >
            {error}
          </Alert>
        )}
      </ModalBody>
      <ModalFooter>
        <Button
          variant={confirmVariant}
          onClick={onConfirm}
          isDisabled={!isConfirmEnabled}
          isLoading={isCreating}
        >
          Confirm {label}
        </Button>
        <Button variant="link" onClick={onClose}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
}
