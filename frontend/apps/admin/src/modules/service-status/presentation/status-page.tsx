import { useQuery } from '@tanstack/react-query';
import { Button } from '@mender/ui';
import type { ReadStatus } from '../application/read-status';

export function StatusPage({ readStatus }: { readStatus: ReadStatus }) {
  const status = useQuery({
    queryKey: ['admin', 'service-health'],
    queryFn: ({ signal }) => readStatus(signal),
  });
  const label = status.isPending ? '正在连接服务…' : status.isError ? '暂时无法连接' : '服务连接成功';
  return <>
    <p className="eyebrow">Mender / 服务状态</p>
    <h1>检查服务连接</h1>
    <p className="lead">确认当前页面能否与 Mender 服务通信。</p>
    <section className="status-panel" aria-label="连接状态" aria-busy={status.isFetching}>
      <div role="status" aria-live="polite">
        <h2 className="status-title">{label}</h2>
        <p>{status.isPending ? '请稍候，正在发送连接请求。' : status.isError
          ? '请确认本地 API 已启动，再重试连接。'
          : `已连接到 ${status.data.name}。`}</p>
      </div>
      <div className="status-actions"><Button disabled={status.isFetching} onClick={() => { void status.refetch(); }}>
        {status.isFetching ? '检查中…' : '重新检查'}
      </Button></div>
    </section>
    <p className="status-note">连接成功仅表示服务进程可达。工作空间、身份认证与业务功能尚未开放。</p>
  </>;
}
