import React from 'react';
import { createRoot } from 'react-dom/client';
import StatusPage from './StatusPage';

const el = document.getElementById('root');
if (el) {
  createRoot(el).render(
    <React.StrictMode>
      <StatusPage />
    </React.StrictMode>,
  );
}
