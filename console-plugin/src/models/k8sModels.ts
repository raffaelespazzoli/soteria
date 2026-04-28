import { K8sModel } from '@openshift-console/dynamic-plugin-sdk';

export const drExecutionModel: K8sModel = {
  apiGroup: 'soteria.io',
  apiVersion: 'v1alpha1',
  kind: 'DRExecution',
  abbr: 'DRE',
  label: 'DRExecution',
  labelPlural: 'DRExecutions',
  plural: 'drexecutions',
  namespaced: false,
};
