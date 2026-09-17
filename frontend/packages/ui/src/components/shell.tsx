import type { ReactNode } from 'react';
import { Badge } from './badge';

export function Shell({ label, navigation, children }: {
  label: string;
  navigation: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">跳到主要内容</a>
      <aside className="sidebar">
        <a className="brand" href="/" aria-label={`Mender ${label} 首页`}>
          <span className="brand-mark" aria-hidden="true">m</span>
          <span>Mender<span className="brand-label">{label}</span></span>
        </a>
        <nav aria-label="主导航">{navigation}</nav>
        <div className="sidebar-footer"><span className="small-dot" />Local workspace</div>
      </aside>
      <div className="workspace">
        <header className="topbar"><span>{label}</span><Badge variant="outline">Local</Badge></header>
        <main id="main-content" tabIndex={-1}>{children}</main>
        <footer className="page-footer">Mender<span>Tools for agents, governed by your workspace</span></footer>
      </div>
    </div>
  );
}
