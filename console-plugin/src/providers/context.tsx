import { createContext, useContext } from 'react';
import type { K8sProvider } from './types';
import { ocpProvider } from './ocp';
import { standaloneProvider } from './standalone';

declare const __STANDALONE__: boolean;

const defaultProvider: K8sProvider =
  typeof __STANDALONE__ !== 'undefined' && __STANDALONE__ ? standaloneProvider : ocpProvider;

const K8sProviderContext = createContext<K8sProvider>(defaultProvider);

export const K8sProviderProvider: React.FC<{
  provider?: K8sProvider;
  children: React.ReactNode;
}> = ({ provider, children }) => (
  <K8sProviderContext.Provider value={provider ?? defaultProvider}>
    {children}
  </K8sProviderContext.Provider>
);

export function useProvider(): K8sProvider {
  return useContext(K8sProviderContext);
}
