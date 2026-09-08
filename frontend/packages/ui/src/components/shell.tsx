import type { ReactNode } from 'react';

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
        <div className="sidebar-footer"><span className="small-dot" />初始化预览</div>
      </aside>
      <div className="workspace">
        <header className="topbar"><span>{label}</span><span className="preview-badge">开发版本 · 0.1</span></header>
        <main id="main-content" tabIndex={-1}>{children}</main>
        <footer className="page-footer">Mender<span>连接能力 · 治理调用 · 可靠分发</span></footer>
      </div>
    </div>
  );
}
