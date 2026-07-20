import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Navigate, NavLink, Route, Routes, useNavigate } from 'react-router-dom'
import { api, clearToken, getToken } from './api/client'
import { currentMonth, greeting, money, monthLabel, shiftMonth, ymd, fmtTime } from './lib/format'
import './styles.css'

function clampMonth(ym) {
  const cur = currentMonth()
  if (!ym) return cur
  return ym > cur ? cur : ym
}

function canGoNextMonth(ym) {
  return (ym || currentMonth()) < currentMonth()
}

// Soft but distinct icon color: person color if labeled, else by money direction/kind.
function txnIconColor(t) {
  if (t?.person_color) return t.person_color
  if (t?.direction === 'in') return '#1f7a4c'
  switch (t?.kind) {
    case 'transfer':
      return '#4f6bed'
    case 'fee':
      return '#b7791f'
    case 'invest':
      return '#7c5cbf'
    case 'refund':
      return '#2f9e6b'
    case 'consume':
      return '#d95926'
    default:
      return '#1f6b4a'
  }
}

function kindLabelOf(t) {
  return ({
    consume: '消费',
    transfer: '转账',
    refund: '退款',
    fee: '手续费',
    income: '入账',
    invest: '理财',
    other: '其他',
  })[t?.kind] || t?.category || t?.kind || '—'
}

const ChartsDaily = lazy(() => import('./components/Charts.jsx').then((m) => ({ default: m.ChartsDaily })))
const ChartsDonut = lazy(() => import('./components/Charts.jsx').then((m) => ({ default: m.ChartsDonut })))
const ChartsHBar = lazy(() => import('./components/Charts.jsx').then((m) => ({ default: m.ChartsHBar })))
const ChartsMonthly = lazy(() => import('./components/Charts.jsx').then((m) => ({ default: m.ChartsMonthly })))
const ChartsBalance = lazy(() => import('./components/Charts.jsx').then((m) => ({ default: m.ChartsBalance })))

function LazyChart({ children }) {
  return <Suspense fallback={<div className="empty-chart">图表加载中…</div>}>{children}</Suspense>
}

function useAuth() {
  const [authed, setAuthed] = useState(false)
  const [passwordLogin, setPasswordLogin] = useState(false)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState('')

  const refresh = useCallback(async () => {
    try {
      const me = await api.me()
      setPasswordLogin(!!me.password_login)
      if (me.authenticated) {
        setAuthed(true)
        setReady(true)
        return true
      }
      if (getToken()) {
        // try token path by hitting digest lightly
        try {
          await api.digest()
          setAuthed(true)
          setReady(true)
          return true
        } catch {
          /* fallthrough */
        }
      }
      setAuthed(false)
      setReady(true)
      return false
    } catch {
      setAuthed(!!getToken())
      setReady(true)
      return !!getToken()
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  return { authed, setAuthed, passwordLogin, ready, error, setError, refresh }
}

function LoginPage({ passwordLogin, onSuccess, setError }) {
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)

  async function doLogin() {
    setBusy(true)
    setError('')
    try {
      await api.login(pw)
      onSuccess()
    } catch (e) {
      setError(e.message || '登录失败')
    } finally {
      setBusy(false)
    }
  }

  if (!passwordLogin) {
    return (
      <div className="login-page">
        <div className="login-panel" style={{ gridColumn: '1 / -1' }}>
          <div className="login-form">
            <h2>无法登录</h2>
            <p>服务器未配置 ADMIN_PASSWORD。</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="login-page">
      <div className="login-art">
        <div className="login-logo"><span>¥</span> CashPulse</div>
        <div className="login-quote">
          <span>PRIVATE LEDGER</span>
          <h1>你的私人账本</h1>
          <p>登录后会记住会话，日常打开不用反复输密码。</p>
        </div>
      </div>
      <div className="login-panel">
        <div className="login-form">
          <span className="eyebrow">LOGIN</span>
          <h2>登录</h2>
          <p>仅密码 · 防暴力尝试</p>
          <label>密码</label>
          <input
            id="login-password"
            className="field"
            type="password"
            autoComplete="current-password"
            value={pw}
            onChange={(e) => setPw(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && !busy && pw && doLogin()}
            placeholder="管理密码"
          />
          <button id="btn-login" className="btn primary login-submit" disabled={busy || !pw} onClick={doLogin}>
            {busy ? '登录中…' : '登录'}
          </button>
        </div>
      </div>
    </div>
  )
}

function Shell({ onLogout, children, labelCount = 0 }) {
  const nav = [
    ['/', '今日'],
    ['/txns', '流水'],
    ['/analysis', '分析'],
    ['/organize', '整理'],
    ['/settings', '设置'],
  ]
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-mark">
          <span>¥</span>
          <div><strong>CashPulse</strong><small>个人财务</small></div>
        </div>
        <nav>
          {nav.map(([to, label]) => (
            <NavLink key={to} to={to} end={to === '/'} className={({ isActive }) => (isActive ? 'active' : '')}>
              <span>{label}</span>
              {to === '/organize' && labelCount > 0 ? <b>{labelCount}</b> : null}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <div className="sync-state"><i /><span>连接正常</span></div>
          <button type="button" className="btn ghost small" onClick={onLogout}>退出</button>
        </div>
      </aside>
      <main className="main-content">{children}</main>
    </div>
  )
}

function Home({ data, onRefresh, onNav, onMonth }) {
  const a = data.analytics || {}
  const range = a.range || {}
  const d = data.digest || {}
  const net = (range.income || 0) - (range.expense || 0)
  const notes = d.anomalies || []
  const budgets = data.budgets || []
  const recent = (data.transactions || []).slice(0, 6)

  return (
    <div className="page-enter">
      <header className="welcome">
        <div>
          <p>{greeting()}，这是你的财务近况</p>
          <div className="month-hero-nav">
            <button type="button" className="icon-btn" onClick={() => onMonth(shiftMonth(data.periodMonth || currentMonth(), -1))} aria-label="上个月">‹</button>
            <h2>{monthLabel(data.periodMonth)}</h2>
            <button type="button" className="icon-btn" disabled={!canGoNextMonth(data.periodMonth)} onClick={() => onMonth(clampMonth(shiftMonth(data.periodMonth || currentMonth(), 1)))} aria-label="下个月">›</button>
            <button type="button" className="btn ghost" onClick={() => onMonth(currentMonth())}>本月</button>
            <input
              type="month"
              className="field month-input"
              value={data.periodMonth || currentMonth()}
              max={currentMonth()}
              onChange={(e) => onMonth(clampMonth(e.target.value))}
              title="选择月份"
            />
          </div>
        </div>
        <button className="icon-btn" type="button" onClick={() => onRefresh()} title="刷新">↻</button>
      </header>

      <section className="hero-grid">
        <article className="balance-hero">
          <div className="hero-glow" />
          <div className="balance-top"><span>最近余额</span><span className="live-dot">已同步</span></div>
          <div className="balance-value">{a.balance_known ? money(a.latest_balance) : '暂无余额'}</div>
          <div className="balance-time">{a.latest_balance_at ? `更新于 ${fmtTime(a.latest_balance_at)}${a.latest_card_last4 ? ` · 尾号${a.latest_card_last4}` : ''}` : '等待短信中的余额'}</div>
          <div className="balance-stats">
            <div><span>本月收入</span><strong>+{money(range.income)}</strong></div>
            <div>
              <span>本月支出</span>
              <strong>−{money(range.expense)}</strong>
              {range.refund > 0 ? <em className="stat-sub">已扣退款 {money(range.refund)}</em> : null}
            </div>
            <div><span>本月结余</span><strong className={net < 0 ? 'negative' : ''}>{money(net, true)}</strong></div>
          </div>
        </article>
        <article className="card budget-card">
          <div className="card-head">
            <div><span className="eyebrow">BUDGET</span><h3>预算余量</h3></div>
            <button type="button" className="text-btn" onClick={() => onNav('/settings')}>管理 →</button>
          </div>
          {!budgets.length ? (
            <div className="empty-state compact">
              <strong>给这个月定个边界</strong>
              <p>设置预算后，这里显示还剩多少可用。</p>
            </div>
          ) : budgets.slice(0, 2).map((b) => {
            const left = Math.max(0, b.amount - b.spent)
            const pct = Math.min(100, b.pct || 0)
            const tone = pct >= 100 ? 'danger' : pct >= 80 ? 'warn' : ''
            return (
              <div className="budget-line" key={b.id}>
                <div className="budget-name">
                  <strong>{b.person_name || '全账户'}</strong>
                  <span>{b.kind === 'all' ? '全部支出' : b.kind === 'consume' ? '日常消费' : b.kind === 'transfer' ? '转账' : b.kind}</span>
                </div>
                <div className="budget-number"><strong>{money(left)}</strong><span>可用</span></div>
                <div className={`progress ${tone}`}><span style={{ width: `${pct}%` }} /></div>
                <div className="budget-foot"><span>已用 {money(b.spent)}</span><span>{Math.round(pct)}%</span></div>
              </div>
            )
          })}
        </article>
      </section>

      <section className="quick-grid">
        <article className="quick-stat"><span className="quick-icon coral">今</span><div><span>今天花了</span><strong>{money(d.today_expense ?? a.today?.expense ?? 0)}</strong><small>{d.today_txn_count ?? 0} 笔</small></div></article>
        <article className="quick-stat"><span className="quick-icon blue">均</span><div><span>日均支出</span><strong>{money(range.avg_daily_expense)}</strong><small>{range.active_days || 0} 个有支出日</small></div></article>
        <article className="quick-stat"><span className="quick-icon green">安</span><div><span>资金安全垫</span><strong>{a.balance_health?.days_of_runway ? `${Math.round(a.balance_health.days_of_runway)} 天` : '—'}</strong><small>按支出日均</small></div></article>
        <article className="quick-stat actionable" onClick={() => onNav('/organize')}><span className="quick-icon amber">理</span><div><span>等待整理</span><strong>{(d.unlabeled_week ?? a.unlabeled_count ?? 0)} 笔</strong><small>点此去归类</small></div></article>
      </section>

      <section className="home-content">
        <article className="card">
          <div className="card-head">
            <div><span className="eyebrow">SPENDING</span><h3>本月支出节奏（按金额）</h3></div>
            <button type="button" className="text-btn" onClick={() => onNav('/analysis')}>分析 →</button>
          </div>
          <LazyChart><ChartsDaily daily={a.daily || []} /></LazyChart>
        </article>
        <article className="card focus-card">
          <span className="eyebrow">NOTE</span>
          <h3>值得留意</h3>
          <p>{notes[0] || '本月还没有特别异常'}</p>
          {notes.slice(1, 3).map((n) => <div className="focus-note" key={n}>{n}</div>)}
        </article>
      </section>

      <section className="card">
        <div className="card-head">
          <div><span className="eyebrow">LATEST</span><h3>最近流水</h3></div>
          <button type="button" className="text-btn" onClick={() => onNav('/txns')}>全部 →</button>
        </div>
        <TxnList items={recent} empty="还没有流水" />
      </section>
    </div>
  )
}

function TxnList({ items, empty = '暂无流水' }) {
  if (!items?.length) {
    return <div className="empty-state"><strong>{empty}</strong></div>
  }
  return (
    <div className="txn-list">
      {items.map((t) => {
        const inc = t.direction === 'in'
        const tags = t.tags || []
        const hasPerson = Boolean(t.person_name)
        const hasTags = tags.length > 0
        const icon = txnIconColor(t)
        return (
          <div className="txn-item" key={t.id}>
            <span className={`txn-icon ${inc ? 'income' : ''}`} style={{ '--icon': icon }}>
              {(t.merchant || t.bank || '账').slice(0, 1)}
            </span>
            <div className="txn-copy">
              <div className="txn-title-row">
                <strong>{t.merchant || t.bank || '未知'}</strong>
                <div className={`txn-amount ${inc ? 'income' : ''}`}>{inc ? '+' : '−'}{money(t.amount)}</div>
              </div>
              <span className="txn-meta">{kindLabelOf(t)} · {fmtTime(t.occurred_at)}</span>
              <div className="txn-labels">
                {hasPerson ? (
                  <span className="chip person" style={{ '--chip': t.person_color || '#65766d' }}>{t.person_name}</span>
                ) : (
                  !inc ? <span className="chip muted">未标归属</span> : null
                )}
                {hasTags
                  ? tags.map((tag) => (
                      <span className="chip" key={tag.id} style={{ '--chip': tag.color || '#65766d' }}>{tag.name}</span>
                    ))
                  : null}
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}

function Transactions({ data, onSearch, onRefresh, onLoadMore, total = 0, loadingMore = false }) {
  const [q, setQ] = useState(data.search || '')
  const groups = useMemo(() => {
    const m = new Map()
    for (const t of data.transactions || []) {
      const d = new Date(t.occurred_at)
      const key = Number.isNaN(d.getTime()) ? '其他' : ymd(d)
      if (!m.has(key)) m.set(key, [])
      m.get(key).push(t)
    }
    return [...m]
  }, [data.transactions])

  return (
    <div className="page-enter narrow-page">
      <div className="page-title">
        <div><span className="eyebrow">LEDGER</span><h2>全部流水</h2><p>按发生日期排列 · 柱状图看金额不看笔数</p></div>
        <button className="icon-btn" type="button" onClick={() => onRefresh()}>↻</button>
      </div>
      <section className="search-bar">
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="搜索商户、分类…" onKeyDown={(e) => e.key === 'Enter' && onSearch(q)} />
        <button type="button" className="btn primary" onClick={() => onSearch(q)}>搜索</button>
      </section>
      <div className="result-meta">{data.search ? `「${data.search}」 · ` : ''}已显示 {data.transactions?.length || 0}{total ? ` / 共 ${total}` : ''} 笔</div>
      <section className="card">
        {groups.length ? groups.map(([date, items]) => (
          <div className="txn-group" key={date}>
            <div className="txn-date"><strong>{date}</strong><span>{items.length} 笔</span></div>
            <TxnList items={items} />
          </div>
        )) : <div className="empty-state"><strong>没有找到流水</strong></div>}
        {total > (data.transactions?.length || 0) ? (
          <div style={{ padding: 16, textAlign: 'center' }}>
            <button type="button" className="btn primary" disabled={loadingMore} onClick={() => onLoadMore?.()}>
              {loadingMore ? '加载中…' : '加载更多'}
            </button>
          </div>
        ) : null}
      </section>
    </div>
  )
}

function Analysis({ data, period, setPeriod, onRefresh }) {
  const a = data.analytics || {}
  const range = a.range || {}
  const prev = a.prev_range || {}
  const change = prev.expense ? ((range.expense - prev.expense) / prev.expense) * 100 : null
  const peopleAll = a.by_person || []
  const people = peopleAll.filter((p) => p.person_id && p.expense > 0)

  return (
    <div className="page-enter">
      <div className="page-title">
        <div><span className="eyebrow">INSIGHTS</span><h2>消费分析</h2><p>默认看消费金额，转账可单独切换。</p></div>
        <button className="icon-btn" type="button" onClick={() => onRefresh()}>↻</button>
      </div>

      <section className="analysis-controls">
        <div className="month-hero-nav solid">
          <button type="button" className="icon-btn" onClick={() => setPeriod({ ...period, mode: 'month', month: shiftMonth(period.month || currentMonth(), -1) })}>‹</button>
          <strong className="month-title">{monthLabel(period.mode === 'month' ? (period.month || currentMonth()) : (period.month || currentMonth()))}</strong>
          <button type="button" className="icon-btn" disabled={!canGoNextMonth(period.month)} onClick={() => setPeriod({ ...period, mode: 'month', month: clampMonth(shiftMonth(period.month || currentMonth(), 1)) })}>›</button>
          <button type="button" className="btn ghost" onClick={() => setPeriod({ ...period, mode: 'month', month: currentMonth() })}>本月</button>
          <input
            type="month"
            className="field month-input"
            value={period.month || currentMonth()}
            max={currentMonth()}
            onChange={(e) => setPeriod({ ...period, mode: 'month', month: clampMonth(e.target.value) })}
          />
        </div>
        <div className="seg">
          <button type="button" className={period.mode === 'month' ? 'active' : ''} onClick={() => setPeriod({ ...period, mode: 'month', month: period.month || currentMonth() })}>按月</button>
          {[
            [7, '7 天'],
            [15, '15 天'],
            [30, '30 天'],
            [90, '90 天'],
            [0, '全部'],
          ].map(([n, lab]) => (
            <button
              key={n}
              type="button"
              className={period.mode === 'preset' && period.preset === n ? 'active' : ''}
              onClick={() => setPeriod({ mode: 'preset', preset: n, kind: period.kind, month: period.month, from: period.from, to: period.to })}
            >
              {lab}
            </button>
          ))}
        </div>
        <div className="seg">
          {[
            ['all', '全部支出'],
            ['consume', '仅消费'],
            ['transfer', '仅转账'],
          ].map(([k, lab]) => (
            <button
              key={k}
              type="button"
              className={period.kind === k ? 'active' : ''}
              onClick={() => setPeriod({ ...period, kind: k })}
            >
              {lab}
            </button>
          ))}
        </div>
      </section>

      <section className="analysis-kpis">
        <div>
          <span>区间净支出</span>
          <strong>{money(range.expense)}</strong>
          <small className={change > 0 ? 'bad' : 'good'}>
            {change == null ? '暂无对比' : `较上期 ${change > 0 ? '+' : ''}${change.toFixed(1)}%`}
            {range.refund > 0 ? ` · 退款 −${money(range.refund)}` : ''}
          </small>
        </div>
        <div><span>区间收入</span><strong>{money(range.income)}</strong><small>{range.income_count || 0} 笔（不含退款）</small></div>
        <div><span>日均（金额）</span><strong>{money(range.avg_daily_expense)}</strong><small>{range.active_days || 0} 个有支出日</small></div>
        <div><span>结余</span><strong>{money((range.income || 0) - (range.expense || 0), true)}</strong><small>{range.txn_count || 0} 笔</small></div>
      </section>

      <section className="analysis-grid">
        <article className="card wide">
          <div className="card-head"><div><span className="eyebrow">TREND</span><h3>每日支出（柱高 = 花的钱）</h3></div><span className="hint">{a.from} — {a.to}</span></div>
          <LazyChart><ChartsDaily daily={a.daily || []} height={300} /></LazyChart>
        </article>
        <article className="card">
          <div className="card-head"><div><span className="eyebrow">MIX</span><h3>渠道构成</h3></div></div>
          <LazyChart><ChartsDonut rows={a.by_channel || []} /></LazyChart>
        </article>
        <article className="card">
          <div className="card-head"><div><span className="eyebrow">RANK</span><h3>渠道排行</h3></div></div>
          <LazyChart><ChartsHBar rows={a.by_channel || []} /></LazyChart>
        </article>
        <article className="card">
          <div className="card-head"><div><span className="eyebrow">TIME</span><h3>{(a.monthly || []).length > 1 ? '月度收支' : '余额'}</h3></div></div>
          {(a.monthly || []).length > 1 ? <LazyChart><ChartsMonthly monthly={a.monthly || []} /></LazyChart> : <LazyChart><ChartsBalance series={a.balance_series || []} /></LazyChart>}
        </article>
        <article className="card wide">
          <div className="card-head">
            <div>
              <span className="eyebrow">PEOPLE</span>
              <h3>谁花了多少</h3>
            </div>
            <span className="hint">按归属人汇总当前区间 · 仅统计已打标流水中的支出金额</span>
          </div>
          {peopleAll.length ? (
            <>
              {people.length ? (
                <LazyChart><ChartsHBar rows={people} nameKey="person_name" height={Math.max(180, people.length * 46)} /></LazyChart>
              ) : (
                <div className="empty-chart">当前筛选下暂无「已归属」支出，下面表格仍含未标记</div>
              )}
              <div className="person-table-wrap">
                <table className="person-table">
                  <thead>
                    <tr>
                      <th>归属</th>
                      <th>支出金额</th>
                      <th>收入</th>
                      <th>笔数</th>
                      <th>占区间支出</th>
                    </tr>
                  </thead>
                  <tbody>
                    {peopleAll.map((p) => (
                      <tr key={p.person_id ?? 'unlabeled'}>
                        <td>
                          <span className="person-dot" style={{ background: p.color || '#8a897c' }} />
                          {p.person_name || '未标记'}
                        </td>
                        <td className="num exp">{money(p.expense)}</td>
                        <td className="num">{money(p.income)}</td>
                        <td className="num">{p.txn_count}</td>
                        <td className="num">{p.pct != null ? `${p.pct}%` : '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p className="hint" style={{ marginTop: 10 }}>
                  提示：未标记的支出会单独一行。在「整理」里给流水打上「我 / 老婆 / 孩子」后，这里会按人分开。
                </p>
              </div>
            </>
          ) : (
            <div className="empty-state">
              <strong>还没有可汇总的流水</strong>
              <p>先保证当前区间有数据；有支出后会显示「未标记 / 各归属人」金额表。</p>
            </div>
          )}
        </article>
      </section>
    </div>
  )
}

function Organize({ data, onRefresh, onCountersRefresh, onLabel }) {
  const [window, setWindow] = useState('today')
  const [onlyUnlabeled, setOnlyUnlabeled] = useState(true)
  const [queue, setQueue] = useState([])
  // Items kept on screen after person assign under「只看未归类」, so tags can still be tapped.
  // Keyed by txn id. Cleared when filter/window changes or user taps「完成」.
  const [held, setHeld] = useState({})
  const heldRef = useRef({})
  const [busyId, setBusyId] = useState(null)
  const [loadErr, setLoadErr] = useState('')
  const [actionErr, setActionErr] = useState('')

  useEffect(() => {
    heldRef.current = held
  }, [held])

  const range = useMemo(() => {
    const now = new Date()
    const end = ymd(now)
    if (window === 'today') return { from: end, to: end, label: '今天' }
    const s = new Date(now)
    if (window === 'yesterday') {
      s.setDate(s.getDate() - 1)
      const d = ymd(s)
      return { from: d, to: d, label: '昨天' }
    }
    s.setDate(s.getDate() - (window === '3d' ? 2 : 6))
    return { from: ymd(s), to: end, label: window === '3d' ? '近 3 天' : '近 7 天' }
  }, [window])

  // Changing date window / unlabeled filter: drop held cards (fresh inbox).
  useEffect(() => {
    setHeld({})
    heldRef.current = {}
  }, [onlyUnlabeled, range.from, range.to])

  const load = useCallback(async () => {
    const res = await api.transactions({
      limit: 200,
      unlabeled: onlyUnlabeled,
      from: range.from,
      to: range.to,
    })
    let items = res.items || []
    // Merge held (person just set) so「餐饮」等标签仍可点；否则 reload 会把它们滤掉。
    if (onlyUnlabeled) {
      const byId = new Map(items.map((t) => [t.id, t]))
      for (const h of Object.values(heldRef.current)) {
        if (h?.id != null && !byId.has(h.id)) byId.set(h.id, h)
      }
      items = [...byId.values()].sort((a, b) => {
        const ta = new Date(a.occurred_at).getTime()
        const tb = new Date(b.occurred_at).getTime()
        if (tb !== ta) return tb - ta
        return (b.id || 0) - (a.id || 0)
      })
    }
    setQueue(items)
    return res
  }, [onlyUnlabeled, range.from, range.to])

  useEffect(() => {
    setLoadErr('')
    load().catch((e) => setLoadErr(e.message || '加载失败'))
  }, [load])

  function dismissHeld(txnId) {
    setHeld((prev) => {
      if (!prev[txnId]) return prev
      const next = { ...prev }
      delete next[txnId]
      heldRef.current = next
      return next
    })
    setQueue((prev) => prev.filter((t) => t.id !== txnId))
  }

  // Explicit「不标」: clear tags; if person already chosen under 只看未归类, finish the card.
  async function handleNoTag(txn) {
    setActionErr('')
    setBusyId(txn.id)
    try {
      let updated = txn
      if ((txn.tags || []).length > 0) {
        updated = await onLabel(txn.id, { tag_ids: [] })
        setQueue((prev) => prev.map((t) => (t.id === txn.id ? { ...t, ...updated } : t)))
        if (heldRef.current[txn.id]) {
          setHeld((prev) => {
            if (!prev[txn.id]) return prev
            const next = { ...prev, [txn.id]: updated }
            heldRef.current = next
            return next
          })
        }
        if (onCountersRefresh) await onCountersRefresh()
      }
      const personId = updated.person_id != null ? updated.person_id : txn.person_id
      if (onlyUnlabeled && personId != null) {
        dismissHeld(txn.id)
      }
    } catch (e) {
      setActionErr(e.message || '操作失败，请重试')
      try { await load() } catch { /* ignore */ }
    } finally {
      setBusyId(null)
    }
  }

  async function handleLabel(txnId, body) {
    setActionErr('')
    setBusyId(txnId)
    try {
      const updated = await onLabel(txnId, body)
      const personJustSet =
        onlyUnlabeled
        && body
        && Object.prototype.hasOwnProperty.call(body, 'person_id')
        && body.person_id != null
      const personCleared =
        onlyUnlabeled
        && body
        && Object.prototype.hasOwnProperty.call(body, 'person_id')
        && body.person_id == null

      // Keep local row; never drop on person assign (old bug: card vanished before tags).
      setQueue((prev) => prev.map((t) => (t.id === txnId ? { ...t, ...updated } : t)))

      if (personJustSet) {
        setHeld((prev) => {
          const next = { ...prev, [txnId]: updated }
          heldRef.current = next
          return next
        })
      } else if (personCleared) {
        setHeld((prev) => {
          if (!prev[txnId]) return prev
          const next = { ...prev }
          delete next[txnId]
          heldRef.current = next
          return next
        })
      } else if (heldRef.current[txnId]) {
        // Tag toggles while held: keep freshest server row.
        setHeld((prev) => {
          if (!prev[txnId]) return prev
          const next = { ...prev, [txnId]: updated }
          heldRef.current = next
          return next
        })
      }

      if (onCountersRefresh) await onCountersRefresh()
      // Only re-fetch when not in unlabeled inbox, or after clear person.
      // Person/tag updates already have server `updated`; reload would race and drop held cards.
      if (!onlyUnlabeled || personCleared) {
        await load()
      }
    } catch (e) {
      setActionErr(e.message || '打标失败，请重试')
      try { await load() } catch { /* ignore */ }
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div className="page-enter narrow-page">
      <div className="page-title">
        <div><span className="eyebrow">INBOX</span><h2>整理流水</h2><p>归属必选 · 标签可选（可点「不标」）</p></div>
        <button
          className="icon-btn"
          type="button"
          onClick={() => {
            setLoadErr('')
            load().catch((e) => setLoadErr(e.message || '加载失败'))
            onRefresh?.()
          }}
        >
          ↻
        </button>
      </div>
      <section className="organize-summary">
        <div className="organize-count">
          <strong>{queue.length}</strong>
          <span>{onlyUnlabeled ? '笔待整理' : '笔流水'}</span>
        </div>
        <div>
          <strong>{range.label}</strong>
          <p>{range.from}{range.from !== range.to ? ` — ${range.to}` : ''}</p>
        </div>
      </section>
      {actionErr ? <div className="banner err">{actionErr}</div> : null}
      <section className="card">
        <div className="organize-toolbar">
          <div className="seg">
            {[['today', '今天'], ['yesterday', '昨天'], ['3d', '近3天'], ['7d', '近7天']].map(([id, lab]) => (
              <button key={id} type="button" className={window === id ? 'active' : ''} onClick={() => setWindow(id)}>{lab}</button>
            ))}
          </div>
          <label className="check">
            <input type="checkbox" checked={onlyUnlabeled} onChange={(e) => setOnlyUnlabeled(e.target.checked)} />
            <span>只看未归类</span>
          </label>
        </div>
        {loadErr ? (
          <div className="empty-state"><strong>加载失败</strong><p>{loadErr}</p></div>
        ) : !data.people?.length ? (
          <div className="empty-state"><strong>先添加归属人</strong><p>到设置里加「我 / 老婆 / 孩子」</p></div>
        ) : !queue.length ? (
          <div className="empty-state success"><strong>已经整理完了</strong></div>
        ) : (
          <div className="organize-list">
            {queue.map((t) => {
              const isHeld = Boolean(held[t.id])
              const hasPerson = t.person_id != null
              return (
              <article className={`organize-item ${busyId === t.id ? 'is-busy' : ''} ${isHeld ? 'is-held' : ''}`} key={t.id}>
                <div className="organize-main">
                  <span
                    className={`txn-icon ${t.direction === 'in' ? 'income' : ''}`}
                    style={{ '--icon': txnIconColor(t) }}
                  >
                    {(t.merchant || '账').slice(0, 1)}
                  </span>
                  <div className="txn-copy">
                    <strong>{t.merchant || t.bank || '未知'}</strong>
                    <span>{fmtTime(t.occurred_at)} · {kindLabelOf(t)}</span>
                  </div>
                  <div className={`txn-amount ${t.direction === 'in' ? 'income' : ''}`}>
                    {t.direction === 'in' ? '+' : '−'}{money(t.amount)}
                  </div>
                </div>
                <div className="label-row person-row">
                  <span>归属</span>
                  <div className="chip-group">
                    {(data.people || []).map((p) => (
                      <button
                        key={p.id}
                        type="button"
                        disabled={busyId === t.id}
                        className={`chip-btn ${t.person_id === p.id ? 'active' : ''}`}
                        style={{ '--chip': p.color || '#3987e5' }}
                        onClick={() => handleLabel(t.id, { person_id: p.id })}
                      >
                        {p.name}
                      </button>
                    ))}
                  </div>
                  {hasPerson ? (
                    <button
                      type="button"
                      className="text-btn clear-person"
                      disabled={busyId === t.id}
                      onClick={() => handleLabel(t.id, { person_id: null })}
                    >
                      改归属
                    </button>
                  ) : (
                    <span className="row-spacer" aria-hidden="true" />
                  )}
                </div>
                <div className="label-row">
                  <span>标签</span>
                  <div className="chip-group">
                    <button
                      type="button"
                      disabled={busyId === t.id}
                      className={`chip-btn chip-none ${!(t.tags || []).length ? 'active' : ''}`}
                      onClick={() => {
                        const cur = queue.find((x) => x.id === t.id) || t
                        handleNoTag(cur)
                      }}
                    >
                      不标
                    </button>
                    {(data.tags || []).map((tag) => {
                      const on = (t.tags || []).some((x) => x.id === tag.id)
                      return (
                        <button
                          key={tag.id}
                          type="button"
                          disabled={busyId === t.id}
                          className={`chip-btn ${on ? 'active' : ''}`}
                          style={{ '--chip': tag.color || '#8a897c' }}
                          onClick={() => {
                            // Build from latest queue row (not stale closure if double-taps)
                            const cur = queue.find((x) => x.id === t.id) || t
                            const have = new Set((cur.tags || []).map((x) => x.id))
                            if (have.has(tag.id)) have.delete(tag.id)
                            else have.add(tag.id)
                            handleLabel(t.id, { tag_ids: [...have] })
                          }}
                        >
                          {tag.name}
                        </button>
                      )
                    })}
                  </div>
                </div>
                {onlyUnlabeled && hasPerson ? (
                  <div className="label-row organize-done-row">
                    <span className="hint">
                      {(t.tags || []).length
                        ? '已归属 · 可改标签，或点完成'
                        : '已归属 · 可点标签，或点「不标/完成」'}
                    </span>
                    <button
                      type="button"
                      className="btn primary small"
                      disabled={busyId === t.id}
                      onClick={() => dismissHeld(t.id)}
                    >
                      完成
                    </button>
                  </div>
                ) : null}
              </article>
              )
            })}
          </div>
        )}
      </section>
    </div>
  )
}

function Settings({ data, onRefresh, onLoadExtras, setError, setNotice, budgetMonth }) {
  useEffect(() => { onLoadExtras?.() }, [onLoadExtras])
  const [budgetAmount, setBudgetAmount] = useState('')
  const [budgetKind, setBudgetKind] = useState('all')
  const [budgetPerson, setBudgetPerson] = useState('')
  const [goalName, setGoalName] = useState('')
  const [goalTarget, setGoalTarget] = useState('')
  const [personName, setPersonName] = useState('')
  const [tagName, setTagName] = useState('')
  const [ruleField, setRuleField] = useState('merchant_norm')
  const [ruleOp, setRuleOp] = useState('contains')
  const [ruleValue, setRuleValue] = useState('')
  const [rulePerson, setRulePerson] = useState('')
  const [ruleTag, setRuleTag] = useState('')
  const [exportFrom, setExportFrom] = useState('')
  const [exportTo, setExportTo] = useState('')
  const [sms, setSms] = useState('')
  const [testOut, setTestOut] = useState('')

  return (
    <div className="page-enter narrow-page">
      <div className="page-title">
        <div><span className="eyebrow">SETTINGS</span><h2>设置与自动化</h2><p>预算 · 规则 · 目标 · 导出</p></div>
      </div>

      <section className="card settings-card">
        <h3>月度预算（{budgetMonth || data.periodMonth || currentMonth()}）</h3>
        <div className="form-row">
          <input className="field" type="number" placeholder="金额" value={budgetAmount} onChange={(e) => setBudgetAmount(e.target.value)} />
          <select className="field" value={budgetKind} onChange={(e) => setBudgetKind(e.target.value)}>
            <option value="all">全部支出</option>
            <option value="consume">日常消费</option>
            <option value="transfer">仅转账</option>
          </select>
          <select className="field" value={budgetPerson} onChange={(e) => setBudgetPerson(e.target.value)}>
            <option value="">全账户</option>
            {(data.people || []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
          <button
            type="button"
            className="btn primary"
            onClick={async () => {
              try {
                const m = budgetMonth || data.periodMonth || currentMonth()
                await api.upsertBudget({
                  month: m,
                  amount: Number(budgetAmount),
                  kind: budgetKind,
                  person_id: budgetPerson ? Number(budgetPerson) : null,
                })
                setNotice('预算已保存')
                onRefresh()
              } catch (e) {
                setError(e.message)
              }
            }}
          >
            保存
          </button>
        </div>
        <div className="manage-list">
          {(data.budgets || []).map((b) => (
            <div key={b.id}>
              <span><strong>{b.person_name || '全账户'}</strong><small>{money(b.spent)} / {money(b.amount)} · {Math.round(b.pct || 0)}%</small></span>
              <button type="button" onClick={async () => { await api.deleteBudget(b.id); onRefresh() }}>删除</button>
            </div>
          ))}
        </div>
      </section>

      <section className="card settings-card">
        <h3>存钱目标</h3>
        <div className="form-row">
          <input className="field" placeholder="名称" value={goalName} onChange={(e) => setGoalName(e.target.value)} />
          <input className="field" type="number" placeholder="目标金额" value={goalTarget} onChange={(e) => setGoalTarget(e.target.value)} />
          <button type="button" className="btn primary" onClick={async () => { await api.createGoal(goalName, Number(goalTarget)); setGoalName(''); setGoalTarget(''); onRefresh() }}>添加</button>
        </div>
        <div className="manage-list">
          {(data.goals || []).map((g) => (
            <div key={g.id}>
              <span><strong>{g.name}</strong><small>{money(g.current)} / {money(g.target)} · {Math.round(g.pct || 0)}%</small></span>
              <button type="button" onClick={async () => { try { await api.deleteGoal(g.id); onRefresh() } catch (e) { setError(e.message) } }}>删除</button>
            </div>
          ))}
        </div>
      </section>

      <section className="settings-split">
        <div className="card settings-card">
          <h3>归属人</h3>
          <div className="form-row">
            <input className="field" value={personName} onChange={(e) => setPersonName(e.target.value)} placeholder="我 / 老婆" />
            <button type="button" className="btn primary" onClick={async () => { try { await api.createPerson(personName); setPersonName(''); setNotice('已添加'); onRefresh() } catch (e) { setError(e.message) } }}>添加</button>
          </div>
          <div className="chip-manage">
            {(data.people || []).map((p) => (
              <span key={p.id} style={{ '--chip': p.color }}>{p.name}<button type="button" onClick={async () => { if (!confirm('删除此人？其流水归属会清空。')) return; try { await api.deletePerson(p.id); onRefresh() } catch (e) { setError(e.message) } }}>×</button></span>
            ))}
          </div>
        </div>
        <div className="card settings-card">
          <h3>标签</h3>
          <div className="form-row">
            <input className="field" value={tagName} onChange={(e) => setTagName(e.target.value)} placeholder="餐饮" />
            <button type="button" className="btn primary" onClick={async () => { try { await api.createTag(tagName); setTagName(''); setNotice('已添加'); onRefresh() } catch (e) { setError(e.message) } }}>添加</button>
          </div>
          <div className="chip-manage">
            {(data.tags || []).map((t) => (
              <span key={t.id} style={{ '--chip': t.color }}>{t.name}<button type="button" onClick={async () => { try { await api.deleteTag(t.id); onRefresh() } catch (e) { setError(e.message) } }}>×</button></span>
            ))}
          </div>
        </div>
      </section>

      <section className="card settings-card">
        <h3>自动打标规则</h3>
        <div className="form-row wrap">
          <select className="field" value={ruleField} onChange={(e) => setRuleField(e.target.value)}>
            <option value="merchant_norm">商户</option>
            <option value="merchant">商户原文</option>
            <option value="kind">类型</option>
            <option value="category">分类</option>
            <option value="note">备注</option>
          </select>
          <select className="field" value={ruleOp} onChange={(e) => setRuleOp(e.target.value)}>
            <option value="contains">包含</option>
            <option value="eq">等于</option>
          </select>
          <input className="field grow" value={ruleValue} onChange={(e) => setRuleValue(e.target.value)} placeholder="拼多多" />
          <select className="field" value={rulePerson} onChange={(e) => setRulePerson(e.target.value)}>
            <option value="">不设人</option>
            {(data.people || []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
          <select className="field" value={ruleTag} onChange={(e) => setRuleTag(e.target.value)}>
            <option value="">不设标签</option>
            {(data.tags || []).map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <button
            type="button"
            className="btn primary"
            onClick={async () => {
              await api.createRule({
                match_field: ruleField,
                match_op: ruleOp,
                match_value: ruleValue,
                person_id: rulePerson ? Number(rulePerson) : null,
                tag_id: ruleTag ? Number(ruleTag) : null,
                name: ruleValue,
              })
              setRuleValue('')
              onRefresh()
            }}
          >
            添加规则
          </button>
        </div>
        <div className="manage-list">
          {(data.rules || []).map((r) => (
            <div key={r.id}>
              <span><strong>{r.match_field} {r.match_op} 「{r.match_value}」</strong><small>→ {r.person_name || r.tag_name || '—'}</small></span>
              <button type="button" onClick={async () => { try { await api.deleteRule(r.id); onRefresh() } catch (e) { setError(e.message) } }}>删除</button>
            </div>
          ))}
        </div>
      </section>

      <section className="card settings-card">
        <h3>导出 / 银行卡</h3>
        <div className="form-row">
          <input className="field" type="date" value={exportFrom} onChange={(e) => setExportFrom(e.target.value)} />
          <input className="field" type="date" value={exportTo} onChange={(e) => setExportTo(e.target.value)} />
          <button
            type="button"
            className="btn primary"
            onClick={async () => {
              try {
                const url = api.exportUrl(exportFrom, exportTo)
                const headers = {}
                const tok = getToken()
                if (tok) headers.Authorization = `Bearer ${tok}`
                const res = await fetch(url, { credentials: 'include', headers })
                if (!res.ok) throw new Error('export failed')
                const blob = await res.blob()
                const a = document.createElement('a')
                a.href = URL.createObjectURL(blob)
                a.download = 'cashpulse.csv'
                a.click()
                setNotice('导出已开始')
              } catch (e) { setError(e.message || '导出失败') }
            }}
          >
            下载 CSV
          </button>
        </div>

        <div className="cards-row" style={{ marginTop: 12 }}>
          {(data.cards || []).map((c) => (
            <span key={c.card_last4}>尾号 {c.card_last4} · {c.bank || '卡'} · {c.txn_count} 笔</span>
          ))}
        </div>
      </section>

      <details className="debug-panel">
        <summary>短信调试</summary>
        <textarea className="field" rows={3} value={sms} onChange={(e) => setSms(e.target.value)} placeholder="粘贴【邮储银行】短信" />
        <div className="form-row">
          <button
            type="button"
            className="btn primary"
            onClick={async () => {
              try {
                const r = await api.postSMS(sms)
                setTestOut(JSON.stringify(r, null, 2))
                onRefresh()
              } catch (e) {
                setTestOut(e.message)
              }
            }}
          >
            发送
          </button>
        </div>
        <pre>{testOut}</pre>
      </details>
    </div>
  )
}

export default function App() {
  const auth = useAuth()
  const nav = useNavigate()
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [loading, setLoading] = useState(false)
  // kind=all：统计所有 direction=out（消费+转账+手续费+理财…），不再默认只看 consume
  const [period, setPeriod] = useState({ mode: 'month', preset: 30, month: currentMonth(), kind: 'all', from: '', to: '' })
  const [data, setData] = useState({
    analytics: null,
    transactions: [],
    txnTotal: 0,
    people: [],
    tags: [],
    digest: null,
    budgets: [],
    rules: [],
    goals: [],
    cards: [],
    search: '',
    periodMonth: currentMonth(),
  })
  const [loadingMore, setLoadingMore] = useState(false)
  const loadSeq = useRef(0)



  const analyticsQuery = useCallback(() => {
    const kind = period.kind || 'all'
    if (period.mode === 'month') return { month: period.month || currentMonth(), kind }
    if (period.mode === 'custom' && period.from && period.to) return { from: period.from, to: period.to, kind }
    return { days: period.preset, kind }
  }, [period])

  // Light refresh used after labeling so badge / home "等待整理" stay in sync
  const refreshCounters = useCallback(async () => {
    try {
      const q = analyticsQuery()
      const month = period.mode === 'month' ? (period.month || currentMonth()) : currentMonth()
      const boot = await api.bootstrap({ ...q, month, limit: 50 })
      setData((d) => ({
        ...d,
        digest: boot.digest || d.digest,
        analytics: boot.analytics || d.analytics,
        budgets: boot.budgets || d.budgets,
        people: boot.people || d.people,
        tags: boot.tags || d.tags,
      }))
    } catch {
      /* ignore soft refresh errors */
    }
  }, [analyticsQuery, period.mode, period.month])

  const loadAll = useCallback(async (opts = {}) => {
    const { withSettings = false } = opts
    const seq = ++loadSeq.current
    setLoading(true)
    setError('')
    try {
      const month = period.mode === 'month' ? (period.month || currentMonth()) : currentMonth()
      const search = typeof data.search === 'string' ? data.search : ''
      const q = analyticsQuery()
      // One round-trip for shell/home. Settings extras only when needed.
      const bootOpts = { ...q, month, limit: 50 }
      const tasks = [api.bootstrap(bootOpts)]
      if (withSettings) {
        tasks.push(
          api.rules().catch(() => ({ items: [] })),
          api.goals().catch(() => ({ items: [] })),
          api.cards().catch(() => ({ items: [] })),
        )
      }
      // If user is searching, still need filtered list (bootstrap returns unfiltered recent)
      if (search) {
        tasks.push(api.transactions({ q: search, limit: 50, offset: 0 }))
      }
      const results = await Promise.all(tasks)
      if (seq !== loadSeq.current) return
      const boot = results[0]
      let ri = 1
      let rules = { items: data.rules || [] }
      let goals = { items: data.goals || [] }
      let cards = { items: data.cards || [] }
      if (withSettings) {
        rules = results[ri++] || rules
        goals = results[ri++] || goals
        cards = results[ri++] || cards
      }
      let transactions = boot.transactions || []
      let txnTotal = boot.txn_total || 0
      if (search) {
        const tx = results[ri] || { items: [], total: 0 }
        transactions = tx.items || []
        txnTotal = tx.total || 0
      }
      setData((prev) => ({
        analytics: boot.analytics,
        transactions,
        txnTotal,
        people: boot.people || [],
        tags: boot.tags || [],
        digest: boot.digest,
        budgets: boot.budgets || [],
        rules: rules.items || prev.rules || [],
        goals: goals.items || prev.goals || [],
        cards: cards.items || prev.cards || [],
        search,
        periodMonth: boot.month || month,
      }))
      auth.setAuthed(true)
    } catch (e) {
      if (seq !== loadSeq.current) return
      setError(e.message || '加载失败')
      if (e.status === 401) auth.setAuthed(false)
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [analyticsQuery, auth, data.search, period.mode, period.month])

  const loadSettingsExtras = useCallback(async () => {
    try {
      const [rules, goals, cards] = await Promise.all([
        api.rules().catch(() => ({ items: [] })),
        api.goals().catch(() => ({ items: [] })),
        api.cards().catch(() => ({ items: [] })),
      ])
      setData((d) => ({
        ...d,
        rules: rules.items || [],
        goals: goals.items || [],
        cards: cards.items || [],
      }))
    } catch (e) {
      setError(e.message || '设置数据加载失败')
    }
  }, [])

  useEffect(() => {
    if (auth.authed) loadAll()
  }, [auth.authed, period.mode, period.preset, period.month, period.kind, period.from, period.to]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleLogout() {
    try { await api.logout() } catch { /* ignore */ }
    clearToken()
    auth.setAuthed(false)
    nav('/')
  }

  if (!auth.ready) {
    return <div className="boot">加载中…</div>
  }

  if (!auth.authed) {
    return (
      <>
        {error ? <div className="banner err" style={{ maxWidth: 420, margin: '20px auto' }}>{error}</div> : null}
        <LoginPage
          passwordLogin={auth.passwordLogin}
          setError={setError}
          onSuccess={() => {
            setError('')
            auth.setAuthed(true)
            // loadAll triggered by useEffect when authed flips true
          }}
        />
      </>
    )
  }

  const labelCount = data.digest?.unlabeled_week ?? data.analytics?.unlabeled_count ?? 0

  return (
    <Shell onLogout={handleLogout} labelCount={labelCount}>
      {error ? <div className="banner err">{error}</div> : null}
      {notice ? <div className="banner ok">{notice}</div> : null}
      {loading ? <div className="loading-bar" /> : null}
      <Routes>
        <Route path="/" element={<Home
          data={data}
          onRefresh={() => loadAll()}
          onNav={(path) => nav(path)}
          onMonth={(m) => {
            setPeriod((p) => ({ ...p, mode: 'month', month: clampMonth(m) }))
          }}
        />} />
        <Route
          path="/txns"
          element={(
            <Transactions
              data={data}
              total={data.txnTotal}
              loadingMore={loadingMore}
              onRefresh={() => loadAll()}
              onSearch={async (q) => {
                try {
                  const res = await api.transactions({ q, limit: 50, offset: 0 })
                  setData((d) => ({ ...d, transactions: res.items || [], txnTotal: res.total || 0, search: q }))
                } catch (e) {
                  setError(e.message || '搜索失败')
                }
              }}
              onLoadMore={async () => {
                setLoadingMore(true)
                try {
                  const res = await api.transactions({
                    q: data.search || '',
                    limit: 50,
                    offset: data.transactions.length,
                  })
                  setData((d) => ({
                    ...d,
                    transactions: [...d.transactions, ...(res.items || [])],
                    txnTotal: res.total || d.txnTotal,
                  }))
                } catch (e) {
                  setError(e.message || '加载失败')
                } finally {
                  setLoadingMore(false)
                }
              }}
            />
          )}
        />
        <Route
          path="/analysis"
          element={<Analysis data={data} period={period} setPeriod={setPeriod} onRefresh={() => loadAll()} />}
        />
        <Route
          path="/organize"
          element={(
            <Organize
              data={data}
              onRefresh={() => loadAll()}
              onCountersRefresh={refreshCounters}
              onLabel={async (id, body) => {
                // api.label returns updated transaction
                return api.label(id, body)
              }}
            />
          )}
        />
        <Route
          path="/settings"
          element={(
            <Settings
              data={data}
              onRefresh={() => loadAll({ withSettings: true })}
              onLoadExtras={loadSettingsExtras}
              setError={setError}
              setNotice={setNotice}
              budgetMonth={period.mode === 'month' ? (period.month || currentMonth()) : currentMonth()}
            />
          )}
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Shell>
  )
}
