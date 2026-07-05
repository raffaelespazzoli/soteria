import '@patternfly/patternfly/patternfly.min.css';
import '@patternfly/react-core/dist/styles/base.css';

import * as React from 'react';
import * as ReactDOM from 'react-dom';
import { BrowserRouter } from 'react-router-dom';
import { K8sProviderProvider, standaloneProvider } from '../src/providers';
import StandaloneApp from './StandaloneApp';

const app = (
  <React.StrictMode>
    <K8sProviderProvider provider={standaloneProvider}>
      <BrowserRouter>
        <StandaloneApp />
      </BrowserRouter>
    </K8sProviderProvider>
  </React.StrictMode>
);

const container = document.getElementById('root')!;

if ('createRoot' in ReactDOM) {
  (ReactDOM as unknown as { createRoot: (c: Element) => { render: (el: React.ReactNode) => void } })
    .createRoot(container)
    .render(app);
} else {
  (ReactDOM as unknown as { render: (el: React.ReactNode, c: Element) => void })
    .render(app, container);
}
