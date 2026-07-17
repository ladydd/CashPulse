import './style.css'
import {
  clearToken,
  createGoal,
  createPerson,
  createRule,
  createTag,
  deleteBudget,
  deleteGoal,
  deletePerson,
  deleteRule,
  deleteTag,
  exportCSVUrl,
  fetchBudgets,
  fetchCards,
  fetchDigest,
  fetchGoals,
  fetchMe,
  fetchPeople,
  fetchRules,
  fetchTags,
  fetchTransactions,
  fetchUnparsed,
  fetchAnalytics,
  getToken,
  labelTransaction,
  login,
  logout,
  postSMS,
  setToken,
  upsertBudget,
} from './api.js'
import {
  mountBalance,
  mountDailyExpense,
  mountDoughnut,
  mountHBar,
  mountMonthly,
  teardownCharts,
} from './charts.js'

function currentMonthStr() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

const state = {
  page: 'overview', // overview | label | txns | settings
  // period: month | custom | preset
  periodMode: 'preset',
  periodMonth: currentMonthStr(), // YYYY-MM
  periodFrom: '',
  periodTo: '',
  periodPreset: 7, // default: last 7 continuous days
  // kind: consume (default) | transfer | all | refund | fee
  kindFilter: 'consume',
  analytics: null,
  transactions: [],
  labelQueue: [],
  labelWindow: 'today',
  onlyUnlabeled: true,
  people: [],
  tags: [],
  unparsed: [],
  error: '',
  notice: '',
  loading: false,
  search: '',
  authed: false,
  authMode: 'none',
  passwordLogin: false,
  digest: null,
  budgets: [],
  rules: [],
  goals: [],
  cards: [],
  loginPassword: '',
}


function analyticsQuery() {
  const kind = state.kindFilter || 'consume'
  if (state.periodMode === 'month' && state.periodMonth) {
    return { month: state.periodMonth, kind }
  }
  if (state.periodMode === 'custom' && state.periodFrom && state.periodTo) {
    return { from: state.periodFrom, to: state.periodTo, kind }
  }
  if (state.periodMode === 'preset') {
    return { days: state.periodPreset, kind }
  }
  return { month: currentMonthStr(), kind }
}

function shiftMonth(ym, delta) {
  const [y, m] = ym.split('-').map(Number)
  const d = new Date(y, m - 1 + delta, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function periodTitle() {
  if (state.periodMode === 'month') {
    const [y, m] = state.periodMonth.split('-')
    return `${y}年${Number(m)}月`
  }
  if (state.periodMode === 'custom') {
    return `${state.periodFrom} ~ ${state.periodTo}`
  }
  if (state.periodPreset === 0) return '全部时间'
  return `近 ${state.periodPreset} 天`
}

function pctChange(cur, prev) {
  if (!prev) return null
  return ((cur - prev) / prev) * 100
}

function changeHTML(cur, prev) {
  const p = pctChange(cur, prev)
  if (p == null || !Number.isFinite(p)) return `<span class="delta muted">无对比</span>`
  const sign = p > 0 ? '+' : ''
  const cls = p > 0 ? 'up' : p < 0 ? 'down' : 'muted'
  return `<span class="delta ${cls}">较上期 ${sign}${p.toFixed(1)}%</span>`
}

function money(n, withSign = false) {
  const v = Number(n) || 0
  const abs = Math.abs(v).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
  if (!withSign) return `¥${abs}`
  return v >= 0 ? `+¥${abs}` : `-¥${abs}`
}

function fmtTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function ymd(d) {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function labelDateRange() {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const end = ymd(today)
  if (state.labelWindow === 'today') return { from: end, to: end, label: '今天' }
  if (state.labelWindow === 'yesterday') {
    const y = new Date(today)
    y.setDate(y.getDate() - 1)
    const s = ymd(y)
    return { from: s, to: s, label: '昨天' }
  }
  if (state.labelWindow === '3d') {
    const s = new Date(today)
    s.setDate(s.getDate() - 2)
    return { from: ymd(s), to: end, label: '近 3 天' }
  }
  const s = new Date(today)
  s.setDate(s.getDate() - 6)
  return { from: ymd(s), to: end, label: '近 7 天' }
}

function escapeHtml(s) {
  return String(s ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}

function chartBox(id, empty, height = 240) {
  if (empty) return `<div class="empty">${empty}</div>`
  return `<div class="chart-box" style="height:${height}px"><canvas id="${id}"></canvas></div>`
}

/** Month calendar heatmap for daily expense */
function renderCalendarHeatmap(daily, monthYm) {
  if (!monthYm || !daily?.length) return ''
  const [y, m] = monthYm.split('-').map(Number)
  const first = new Date(y, m - 1, 1)
  const lastDay = new Date(y, m, 0).getDate()
  const startPad = (first.getDay() + 6) % 7 // Mon=0
  const byDate = Object.fromEntries((daily || []).map((d) => [d.date, d]))
  const expenses = (daily || []).map((d) => d.expense || 0).filter((v) => v > 0)
  const max = Math.max(1, ...expenses)

  const cells = []
  for (let i = 0; i < startPad; i++) cells.push(`<div class="cal-cell empty"></div>`)
  for (let day = 1; day <= lastDay; day++) {
    const key = `${y}-${String(m).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    const row = byDate[key]
    const exp = row?.expense || 0
    const intensity = exp > 0 ? Math.max(0.18, exp / max) : 0
    const bg =
      exp > 0
        ? `background: color-mix(in oklab, var(--brand) ${Math.round(intensity * 100)}%, var(--bg-soft))`
        : ''
    cells.push(`
      <div class="cal-cell ${exp ? 'has' : ''}" style="${bg}" title="${key} · 支出 ${money(exp)} · ${row?.txn_count || 0} 笔">
        <span class="d">${day}</span>
        ${exp ? `<span class="v">¥${exp >= 1000 ? (exp / 1000).toFixed(1) + 'k' : exp.toFixed(0)}</span>` : ''}
      </div>`)
  }
  const week = ['一', '二', '三', '四', '五', '六', '日']
  return `
    <div class="cal">
      <div class="cal-head">${week.map((w) => `<div>${w}</div>`).join('')}</div>
      <div class="cal-grid">${cells.join('')}</div>
    </div>`
}

function hasDaily(daily) {
  return (daily || []).some((d) => (d.expense || 0) > 0)
}

function hasMonthly(monthly) {
  return (monthly || []).some((m) => (m.expense || 0) > 0 || (m.income || 0) > 0)
}

function hasBalance(series) {
  return (series || []).length >= 2
}

function hasExpenseRows(rows) {
  return (rows || []).some((r) => (r.expense || 0) > 0)
}

function personChip(t) {
  if (!t.person_id) return `<span class="chip muted">未标</span>`
  return `<span class="chip" style="--chip:${escapeHtml(t.person_color || 'var(--brand)')}">${escapeHtml(t.person_name)}</span>`
}

function tagChips(t) {
  return (t.tags || [])
    .map((tg) => `<span class="chip" style="--chip:${escapeHtml(tg.color || '#9ca3af')}">${escapeHtml(tg.name)}</span>`)
    .join('')
}

function renderTxnList(items, { editable = false, empty = '暂无流水' } = {}) {
  if (!items?.length) return `<div class="empty">${empty}</div>`
  return `
    <ul class="list">
      ${items
        .map((t) => {
          const name = t.merchant || t.bank || '未知'
          const dir = t.direction === 'in' ? 'in' : 'out'
          const sign = dir === 'in' ? '+' : '-'
          const meta = [fmtTime(t.occurred_at), t.category, t.balance_known ? `余 ${money(t.balance_after)}` : '']
            .filter(Boolean)
            .join(' · ')
          return `
            <li class="row">
              <div>
                <div class="title">${escapeHtml(name)} ${personChip(t)} ${tagChips(t)}</div>
                <div class="meta">${escapeHtml(meta)}</div>
                ${
                  editable
                    ? `<div class="actions">
                        <div class="btns">
                          ${state.people
                            .map(
                              (p) => `
                            <button type="button" class="chip-btn ${t.person_id === p.id ? 'active' : ''}"
                              data-action="person" data-txn="${t.id}" data-person="${p.id}"
                              style="--chip:${escapeHtml(p.color)}">${escapeHtml(p.name)}</button>`,
                            )
                            .join('')}
                          <button type="button" class="chip-btn" data-action="person-clear" data-txn="${t.id}">清除</button>
                        </div>
                        ${
                          state.tags.length
                            ? `<div class="btns">
                                ${state.tags
                                  .map((tg) => {
                                    const on = (t.tags || []).some((x) => x.id === tg.id)
                                    return `<button type="button" class="chip-btn ${on ? 'active' : ''}"
                                      data-action="tag-toggle" data-txn="${t.id}" data-tag="${tg.id}"
                                      style="--chip:${escapeHtml(tg.color)}">${escapeHtml(tg.name)}</button>`
                                  })
                                  .join('')}
                              </div>`
                            : ''
                        }
                      </div>`
                    : ''
                }
              </div>
              <div class="amt ${dir}">${sign}${money(t.amount)}</div>
            </li>`
        })
        .join('')}
    </ul>`
}

/* —— pages —— */
function pageOverview() {
  const a = state.analytics
  const range = a?.range
  const prev = a?.prev_range
  const labeledPeople = (a?.by_person || []).filter((p) => p.person_id && p.expense > 0)
  const isMonth = state.periodMode === 'month'
  const showMonthly = (a?.monthly || []).length > 1
  // Continuous daily chart for week/15/30/90 and single-month views
  const showDaily = (a?.days || 0) <= 100 || isMonth

  return `
    <div class="page-head">
      <div>
        <h2>总览</h2>
        <p class="desc">按时间维度看钱花在哪 · 当前：<strong>${escapeHtml(periodTitle())}</strong></p>
      </div>
      <div class="head-actions">
        <button class="btn" id="btn-refresh">${state.loading ? '刷新中…' : '刷新'}</button>
      </div>
    </div>

    <section class="period-bar card">
      <div class="period-row">
        <div class="seg" id="period-mode-seg">
          <button type="button" class="${state.periodMode === 'preset' && state.periodPreset === 7 ? 'active' : ''}" data-period-mode="preset" data-preset="7">近7天</button>
          <button type="button" class="${state.periodMode === 'preset' && state.periodPreset === 15 ? 'active' : ''}" data-period-mode="preset" data-preset="15">近15天</button>
          <button type="button" class="${state.periodMode === 'preset' && state.periodPreset === 30 ? 'active' : ''}" data-period-mode="preset" data-preset="30">近30天</button>
          <button type="button" class="${state.periodMode === 'preset' && state.periodPreset === 90 ? 'active' : ''}" data-period-mode="preset" data-preset="90">近90天</button>
          <button type="button" class="${state.periodMode === 'month' ? 'active' : ''}" data-period-mode="month">按月</button>
          <button type="button" class="${state.periodMode === 'preset' && state.periodPreset === 0 ? 'active' : ''}" data-period-mode="preset" data-preset="0">全部</button>
          <button type="button" class="${state.periodMode === 'custom' ? 'active' : ''}" data-period-mode="custom">自定义</button>
        </div>
        <div class="seg" id="kind-seg">
          ${[
            ['consume', '仅消费'],
            ['transfer', '仅转账'],
            ['all', '全部类型'],
          ]
            .map(
              ([k, lab]) =>
                `<button type="button" class="${state.kindFilter === k ? 'active' : ''}" data-kind="${k}">${lab}</button>`,
            )
            .join('')}
        </div>
      </div>
      <div class="period-row controls">
        ${
          state.periodMode === 'month'
            ? `
          <button class="btn" id="btn-month-prev" title="上一月">‹</button>
          <input class="field month-input" type="month" id="period-month" value="${escapeHtml(state.periodMonth)}" />
          <button class="btn" id="btn-month-next" title="下一月">›</button>
          <button class="btn ghost" id="btn-month-now">本月</button>
        `
            : state.periodMode === 'custom'
              ? `
          <label class="field-label">从</label>
          <input class="field" type="date" id="period-from" value="${escapeHtml(state.periodFrom)}" />
          <label class="field-label">到</label>
          <input class="field" type="date" id="period-to" value="${escapeHtml(state.periodTo)}" />
          <button class="btn primary" id="btn-period-apply">应用</button>
        `
              : `<span class="period-hint">预设区间：${escapeHtml(periodTitle())}（${escapeHtml(a?.from || '')} ~ ${escapeHtml(a?.to || '')}）</span>`
        }
      </div>
    </section>

    <section class="kpis">
      <div class="kpi">
        <div class="l">区间支出</div>
        <div class="v exp">${money(range?.expense ?? 0)}</div>
        <div class="s">${changeHTML(range?.expense ?? 0, prev?.expense ?? 0)} · ${range?.expense_count ?? 0} 笔</div>
      </div>
      <div class="kpi">
        <div class="l">区间收入</div>
        <div class="v inc">${money(range?.income ?? 0)}</div>
        <div class="s">${changeHTML(range?.income ?? 0, prev?.income ?? 0)} · ${range?.income_count ?? 0} 笔</div>
      </div>
      <div class="kpi">
        <div class="l">净额 / 日均支出</div>
        <div class="v">${money((range?.income ?? 0) - (range?.expense ?? 0), true)}</div>
        <div class="s">有消费日均 ${money(range?.avg_daily_expense ?? 0)} · ${range?.active_days ?? 0} 天</div>
      </div>
      <div class="kpi">
        <div class="l">安全垫</div>
        <div class="v">${
          a?.balance_health?.days_of_runway
            ? `${a.balance_health.days_of_runway}<span style="font-size:0.9rem;font-weight:600"> 天</span>`
            : a?.balance_known
              ? money(a.latest_balance)
              : '—'
        }</div>
        <div class="s">
          ${
            a?.balance_health?.sample_count
              ? `余额可撑天数 · 最低 ${money(a.balance_health.min_balance)} · 触底(≤${money(a.balance_health.low_threshold)}) ${a.balance_health.low_hit_count} 次`
              : `今日支出 ${money(a?.today?.expense ?? 0)}`
          }
        </div>
      </div>
    </section>

    ${
      a?.balance_health?.sample_count
        ? `<section class="card" style="margin-bottom:12px">
            <div class="card-h"><h3>余额健康</h3><span class="hint">按区间短信余额样本 · 撑天数=最新余额÷消费日均</span></div>
            <div class="kpis" style="margin:0">
              <div class="kpi" style="box-shadow:none">
                <div class="l">最低余额</div>
                <div class="v exp" style="font-size:1.25rem">${money(a.balance_health.min_balance)}</div>
              </div>
              <div class="kpi" style="box-shadow:none">
                <div class="l">平均余额</div>
                <div class="v" style="font-size:1.25rem">${money(a.balance_health.avg_balance)}</div>
              </div>
              <div class="kpi" style="box-shadow:none">
                <div class="l">最高余额</div>
                <div class="v inc" style="font-size:1.25rem">${money(a.balance_health.max_balance)}</div>
              </div>
              <div class="kpi" style="box-shadow:none">
                <div class="l">触底次数</div>
                <div class="v" style="font-size:1.25rem;color:var(--warn)">${a.balance_health.low_hit_count}</div>
                <div class="s">阈值 ${money(a.balance_health.low_threshold)}</div>
              </div>
            </div>
          </section>`
        : ''
    }

    ${
      isMonth
        ? `<section class="card" style="margin-bottom:12px">
            <div class="card-h">
              <h3>支出日历</h3>
              <span class="hint">颜色越深花得越多 · 悬停看金额</span>
            </div>
            ${renderCalendarHeatmap(a?.daily || [], state.periodMonth)}
          </section>`
        : ''
    }

    <div class="g2">
      <section class="card">
        <div class="card-h">
          <h3>每日支出（按金额）</h3>
          <span class="hint">${
            state.periodMode === 'preset' && state.periodPreset > 0
              ? `连续 ${state.periodPreset} 天 · 柱高=当天花的钱`
              : isMonth
                ? '柱高=当天花的钱 · 不是笔数'
                : '柱高=当天花的钱 · 不是笔数'
          }</span>
        </div>
        ${chartBox('chart-daily', hasDaily(a?.daily) ? '' : '这段时间没有支出', 280)}
      </section>
      <section class="card">
        <div class="card-h">
          <h3>渠道构成</h3>
          <span class="hint">区间支出占比</span>
        </div>
        ${chartBox('chart-channel-pie', hasExpenseRows(a?.by_channel) ? '' : '暂无渠道数据', 280)}
      </section>
    </div>

    <div class="g2 eq" style="margin-top:12px">
      <section class="card">
        <div class="card-h">
          <h3>渠道排行</h3>
          <span class="hint">横轴：金额</span>
        </div>
        ${chartBox(
          'chart-channel',
          hasExpenseRows(a?.by_channel) ? '' : '暂无数据',
          Math.max(220, Math.min(400, ((a?.by_channel || []).filter((x) => x.expense > 0).length || 3) * 34)),
        )}
      </section>
      <section class="card">
        <div class="card-h">
          <h3>${showMonthly ? '月度收支' : '账户余额'}</h3>
          <span class="hint">${showMonthly ? '红=支出 · 绿=收入' : '短信回写余额'}</span>
        </div>
        ${
          showMonthly
            ? chartBox('chart-monthly', hasMonthly(a?.monthly) ? '' : '暂无月度', 280)
            : chartBox('chart-balance', hasBalance(a?.balance_series) ? '' : '余额点不足', 280)
        }
      </section>
    </div>

    ${
      showMonthly
        ? `<section class="card" style="margin-top:12px">
            <div class="card-h"><h3>账户余额</h3><span class="hint">区间内短信回写 · 纵轴：余额</span></div>
            ${chartBox('chart-balance', hasBalance(a?.balance_series) ? '' : '余额点不足', 240)}
          </section>`
        : ''
    }

    ${
      labeledPeople.length
        ? `<section class="card" style="margin-top:12px">
            <div class="card-h"><h3>谁花的多</h3><span class="hint">仅已打标 · 横轴金额</span></div>
            ${chartBox('chart-person', '', Math.max(140, labeledPeople.length * 42))}
          </section>`
        : ''
    }
  `
}

function pageLabel() {
  const win = labelDateRange()
  return `
    <div class="page-head">
      <div>
        <h2>每日打标</h2>
        <p class="desc">只处理新流水 · 漏了就切到昨天 / 近几天</p>
      </div>
      <div class="head-actions">
        <button class="btn" id="btn-refresh">${state.loading ? '刷新中…' : '刷新'}</button>
      </div>
    </div>

    <section class="card">
      <div class="toolbar">
        <div class="seg" id="label-seg">
          ${[
            ['today', '今天'],
            ['yesterday', '昨天'],
            ['3d', '近3天'],
            ['7d', '近7天'],
          ]
            .map(
              ([k, lab]) =>
                `<button type="button" class="${state.labelWindow === k ? 'active' : ''}" data-label-window="${k}">${lab}</button>`,
            )
            .join('')}
        </div>
        <label class="check">
          <input type="checkbox" id="only-unlabeled" ${state.onlyUnlabeled ? 'checked' : ''} />
          只看未标记
        </label>
      </div>
      <div class="meta" style="color:var(--text-3);font-size:0.82rem;margin-bottom:10px">
        ${escapeHtml(win.label)}
        · ${escapeHtml(win.from)}${win.from !== win.to ? ' ~ ' + escapeHtml(win.to) : ''}
        · ${state.labelQueue.length} 笔
      </div>
      ${
        !state.people.length
          ? `<div class="empty"><strong>还没有归属人</strong>先去「设置」添加，例如：我 / 老婆 / 孩子</div>`
          : renderTxnList(state.labelQueue, {
              editable: true,
              empty: state.onlyUnlabeled
                ? `<strong>${escapeHtml(win.label)}已清完</strong>没有未标记流水`
                : `${escapeHtml(win.label)}没有流水`,
            })
      }
    </section>
  `
}

function pageTxns() {
  return `
    <div class="page-head">
      <div>
        <h2>流水</h2>
        <p class="desc">最近记录 · 可搜索商户 / 分类</p>
      </div>
      <div class="head-actions">
        <button class="btn" id="btn-refresh">${state.loading ? '刷新中…' : '刷新'}</button>
      </div>
    </div>
    <section class="card">
      <div class="toolbar">
        <input class="field" id="search-q" type="search" placeholder="搜索商户、分类、银行…" value="${escapeHtml(state.search)}" />
        <button class="btn primary" id="btn-search">搜索</button>
      </div>
      ${renderTxnList(state.transactions, { editable: false, empty: '暂无流水' })}
    </section>
  `
}

function pageSettings() {
  const month = currentMonthStr()
  return `
    <div class="page-head">
      <div>
        <h2>设置</h2>
        <p class="desc">预算 · 规则 · 目标 · 导出 · 账号</p>
      </div>
      <div class="head-actions">
        <button class="btn" id="btn-logout">退出登录</button>
      </div>
    </div>

    <section class="card" style="margin-bottom:12px">
      <div class="card-h"><h3>本月预算</h3><span class="hint">${month}</span></div>
      <div class="toolbar">
        <input class="field" id="budget-amount" type="number" placeholder="金额 如 5000" />
        <select class="field" id="budget-kind"><option value="consume">消费</option><option value="all">全部支出</option><option value="transfer">转账</option></select>
        <select class="field" id="budget-person"><option value="">全账户</option>${state.people.map(p=>`<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('')}</select>
        <button class="btn primary" id="btn-save-budget">保存预算</button>
      </div>
      <ul class="manage">
        ${(state.budgets||[]).length ? state.budgets.map(b => `
          <li>
            <span>${escapeHtml(b.person_name || '全账户')} · ${escapeHtml(b.kind)} · 预算 ${money(b.amount)} · 已花 ${money(b.spent)} (${b.pct}%)</span>
            <button class="btn ghost" data-del-budget="${b.id}">删除</button>
          </li>`).join('') : `<div class="empty">尚未设置预算</div>`}
      </ul>
    </section>

    <section class="card" style="margin-bottom:12px">
      <div class="card-h"><h3>自动打标规则</h3><span class="hint">新短信入库时匹配</span></div>
      <div class="toolbar" style="flex-wrap:wrap">
        <select class="field" id="rule-field">
          <option value="merchant_norm">商户(归一)</option>
          <option value="merchant">商户原文</option>
          <option value="kind">类型 kind</option>
          <option value="category">分类</option>
          <option value="note">备注</option>
        </select>
        <select class="field" id="rule-op"><option value="eq">等于</option><option value="contains">包含</option></select>
        <input class="field" id="rule-value" placeholder="如 拼多多" />
        <select class="field" id="rule-person"><option value="">不设人</option>${state.people.map(p=>`<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('')}</select>
        <select class="field" id="rule-tag"><option value="">不设标签</option>${state.tags.map(tg=>`<option value="${tg.id}">${escapeHtml(tg.name)}</option>`).join('')}</select>
        <button class="btn primary" id="btn-add-rule">添加规则</button>
      </div>
      <ul class="manage">
        ${(state.rules||[]).length ? state.rules.map(r => `
          <li>
            <span>${escapeHtml(r.match_field)} ${escapeHtml(r.match_op)} "${escapeHtml(r.match_value)}" → ${escapeHtml(r.person_name||'')} ${escapeHtml(r.tag_name||'')}</span>
            <button class="btn ghost" data-del-rule="${r.id}">删除</button>
          </li>`).join('') : `<div class="empty">无规则</div>`}
      </ul>
    </section>

    <section class="card" style="margin-bottom:12px">
      <div class="card-h"><h3>存钱目标</h3><span class="hint">对照最新余额</span></div>
      <div class="toolbar">
        <input class="field" id="goal-name" placeholder="名称 如 应急金" />
        <input class="field" id="goal-target" type="number" placeholder="目标金额" />
        <button class="btn primary" id="btn-add-goal">添加</button>
      </div>
      <ul class="manage">
        ${(state.goals||[]).length ? state.goals.map(g => `
          <li>
            <span>${escapeHtml(g.name)} · 目标 ${money(g.target)} · 当前 ${money(g.current)} (${g.pct}%)</span>
            <button class="btn ghost" data-del-goal="${g.id}">删除</button>
          </li>`).join('') : `<div class="empty">无目标</div>`}
      </ul>
    </section>

    <div class="g2 eq" style="margin-bottom:12px">
      <section class="card">
        <div class="card-h"><h3>卡片</h3><span class="hint">按短信尾号汇总</span></div>
        <ul class="manage">
          ${(state.cards||[]).length ? state.cards.map(c => `
            <li><span>尾号 ${escapeHtml(c.card_last4)} · ${escapeHtml(c.bank||'')} · ${c.txn_count} 笔 · 支 ${money(c.expense)}</span></li>
          `).join('') : `<div class="empty">暂无</div>`}
        </ul>
      </section>
      <section class="card">
        <div class="card-h"><h3>导出 CSV</h3></div>
        <div class="toolbar">
          <input class="field" type="date" id="export-from" />
          <input class="field" type="date" id="export-to" />
          <a class="btn primary" id="btn-export" href="#">下载</a>
        </div>
        <p class="txn-meta">导出当前登录态可读的流水（UTF-8 BOM，Excel 可开）</p>
      </section>
    </div>

    <div class="g2 eq">
      <section class="card">
        <div class="card-h"><h3>归属人</h3></div>
        <div class="toolbar">
          <input class="field" id="new-person" placeholder="例如：我 / 老婆" />
          <button class="btn primary" id="btn-add-person">添加</button>
        </div>
        <ul class="manage">
          ${state.people.length ? state.people.map(p => `
            <li><span class="chip" style="--chip:${escapeHtml(p.color)};margin:0">${escapeHtml(p.name)}</span>
            <button class="btn ghost" data-del-person="${p.id}">删除</button></li>`).join('') : `<div class="empty">空</div>`}
        </ul>
      </section>
      <section class="card">
        <div class="card-h"><h3>标签</h3></div>
        <div class="toolbar">
          <input class="field" id="new-tag" placeholder="例如：餐饮" />
          <button class="btn primary" id="btn-add-tag">添加</button>
        </div>
        <ul class="manage">
          ${state.tags.length ? state.tags.map(tg => `
            <li><span class="chip" style="--chip:${escapeHtml(tg.color)};margin:0">${escapeHtml(tg.name)}</span>
            <button class="btn ghost" data-del-tag="${tg.id}">删除</button></li>`).join('') : `<div class="empty">空</div>`}
        </ul>
      </section>
    </div>

    <section class="card" style="margin-top:12px">
      <div class="card-h"><h3>调试短信</h3></div>
      <textarea class="field" id="test-sms" rows="3" style="width:100%;font-family:var(--mono);font-size:0.84rem" placeholder="【邮储银行】..."></textarea>
      <div style="margin-top:10px;display:flex;gap:8px">
        <button class="btn primary" id="btn-send-sms">发送</button>
        <button class="btn" id="btn-load-unparsed">未解析</button>
      </div>
      <pre id="test-result" style="margin-top:10px;font-size:0.8rem;color:var(--text-3);white-space:pre-wrap"></pre>
    </section>
  `
}




function pageDigest() {
  const d = state.digest
  return `
    <div class="page-head">
      <div>
        <h2>日报</h2>
        <p class="desc">今日 / 近7日摘要 · 异常提示</p>
      </div>
      <div class="head-actions">
        <button class="btn" id="btn-refresh">${state.loading ? '刷新中…' : '刷新'}</button>
      </div>
    </div>
    ${!d ? `<div class="empty">加载中…</div>` : `
    <section class="kpis">
      <div class="kpi"><div class="l">今日消费</div><div class="v exp">${money(d.today_consume)}</div><div class="s">总支出 ${money(d.today_expense)} · ${d.today_txn_count} 笔</div></div>
      <div class="kpi"><div class="l">近7日消费</div><div class="v exp">${money(d.week_consume)}</div><div class="s">总支出 ${money(d.week_expense)}</div></div>
      <div class="kpi"><div class="l">余额 / 可撑</div><div class="v">${d.balance_known ? money(d.latest_balance) : '—'}</div><div class="s">${d.days_of_runway ? d.days_of_runway + ' 天' : '—'}</div></div>
      <div class="kpi"><div class="l">未打标</div><div class="v" style="color:var(--warn)">${d.unlabeled_today}</div><div class="s">近7日未标 ${d.unlabeled_week}</div></div>
    </section>
    <section class="card" style="margin-bottom:12px">
      <div class="card-h"><h3>值得注意</h3></div>
      ${!(d.anomalies||[]).length ? `<div class="empty">暂无异常</div>` : `<ul class="list">${d.anomalies.map(a=>`<li class="row" style="grid-template-columns:1fr"><div class="title">${escapeHtml(a)}</div></li>`).join('')}</ul>`}
    </section>
    <section class="card">
      <div class="card-h"><h3>今日大额</h3></div>
      ${renderTxnList(d.top_today || [], { empty: '今日无支出' })}
    </section>
    `}
  `
}

function pageLogin() {
  return `
    <div class="login-wrap">
      <div class="card login-card">
        <h2 style="margin:0 0 6px">CashPulse</h2>
        <p class="desc" style="margin:0 0 16px">管理端登录 · 公网访问请使用管理员密码</p>
        ${state.passwordLogin ? `
          <label class="field-label">管理员密码</label>
          <input class="field" id="login-password" type="password" placeholder="ADMIN_PASSWORD" style="width:100%;margin:6px 0 12px" />
          <button class="btn primary" id="btn-login" style="width:100%">登录</button>
        ` : `<p class="txn-meta">未配置 ADMIN_PASSWORD 时，可使用管理 Token 进入：</p>`}
        <div style="margin-top:16px;padding-top:14px;border-top:1px solid var(--line)">
          <label class="field-label">或使用 Admin Token（可选）</label>
          <div class="token-box" style="margin-top:6px">
            <input class="field" id="login-token" type="password" placeholder="ADMIN_TOKEN / API_TOKEN" />
            <button class="btn" id="btn-login-token">使用 Token</button>
          </div>
        </div>
      </div>
    </div>
  `
}

function mountOverviewCharts() {
  if (state.page !== 'overview' || !state.analytics) return
  const a = state.analytics
  const showMonthly = (a.monthly || []).length > 1
  if (hasDaily(a.daily)) mountDailyExpense('chart-daily', a.daily)
  if (hasExpenseRows(a.by_channel)) {
    mountDoughnut('chart-channel-pie', a.by_channel, 'name')
    mountHBar('chart-channel', a.by_channel, 'name')
  }
  if (showMonthly && hasMonthly(a.monthly)) mountMonthly('chart-monthly', a.monthly)
  if (hasBalance(a.balance_series)) mountBalance('chart-balance', a.balance_series)
  const labeledPeople = (a.by_person || []).filter((p) => p.person_id && p.expense > 0)
  if (labeledPeople.length) mountHBar('chart-person', labeledPeople, 'person_name')
}

function render() {
  teardownCharts()
  const app = document.getElementById('app')
  const hasToken = Boolean(getToken())
  if (state.page === 'login' || !state.authed) {
    app.innerHTML = `
      ${state.error ? `<div class="banner err" style="max-width:420px;margin:24px auto">${escapeHtml(state.error)}</div>` : ''}
      ${pageLogin()}
    `
    bindEvents()
    return
  }

  const pages = [
    ['overview', '总览'],
    ['label', '打标'],
    ['txns', '流水'],
    ['digest', '日报'],
    ['settings', '设置'],
  ]

  app.innerHTML = `
    <div class="shell">
      <aside class="nav">
        <div class="brand">
          <h1>CashPulse</h1>
          <p>消费脉搏</p>
        </div>
        ${pages
          .map(
            ([id, lab]) => `
          <button type="button" class="nav-item ${state.page === id ? 'active' : ''}" data-page="${id}">
            ${lab}
          </button>`,
          )
          .join('')}
        <div class="nav-foot">历史看趋势<br/>新账按天打标</div>
      </aside>
      <main class="main">
        ${!hasToken ? `
          <div class="banner err">请先在「设置」里保存 API Token（当前可用 dev-token）</div>
        ` : ''}
        ${state.error ? `<div class="banner err">${escapeHtml(state.error)}</div>` : ''}
        ${state.notice ? `<div class="banner ok">${escapeHtml(state.notice)}</div>` : ''}
        ${
          state.page === 'overview'
            ? pageOverview()
            : state.page === 'label'
              ? pageLabel()
              : state.page === 'txns'
                ? pageTxns()
                : state.page === 'digest'
                  ? pageDigest()
                  : pageSettings()
        }
      </main>
    </div>
  `

  bindEvents()
  // charts need canvas in DOM
  requestAnimationFrame(() => mountOverviewCharts())
}

function findTxnEverywhere(id) {
  for (const list of [state.labelQueue, state.transactions, state.analytics?.top_expenses, state.analytics?.recent_income]) {
    if (!list) continue
    const i = list.findIndex((t) => t.id === id)
    if (i >= 0) return { list, i }
  }
  return null
}

function replaceTxn(updated) {
  for (const list of [state.labelQueue, state.transactions]) {
    const i = list.findIndex((t) => t.id === updated.id)
    if (i >= 0) list[i] = updated
  }
  if (state.onlyUnlabeled && updated.person_id) {
    state.labelQueue = state.labelQueue.filter((t) => t.id !== updated.id)
  }
}

async function onLabelClick(btn) {
  const action = btn.dataset.action
  const txnId = Number(btn.dataset.txn)
  if (!txnId) return
  try {
    let updated
    if (action === 'person') {
      updated = await labelTransaction(txnId, { person_id: Number(btn.dataset.person) })
    } else if (action === 'person-clear') {
      updated = await labelTransaction(txnId, { person_id: null })
    } else if (action === 'tag-toggle') {
      const found = findTxnEverywhere(txnId)
      const current = found ? found.list[found.i] : null
      const tagId = Number(btn.dataset.tag)
      const have = new Set((current?.tags || []).map((t) => t.id))
      if (have.has(tagId)) have.delete(tagId)
      else have.add(tagId)
      updated = await labelTransaction(txnId, { tag_ids: [...have] })
    }
    if (updated) {
      replaceTxn(updated)
      state.error = ''
      state.notice = ''
      render()
      fetchAnalytics(analyticsQuery())
        .then((a) => {
          state.analytics = a
          render()
        })
        .catch(() => {})
    }
  } catch (e) {
    state.error = e.message
    render()
  }
}

function bindEvents() {

  document.getElementById('btn-login')?.addEventListener('click', async () => {
    const pw = document.getElementById('login-password')?.value || ''
    try {
      await login(pw)
      state.authed = true
      state.authMode = 'session'
      state.error = ''
      state.page = 'overview'
      await loadAll()
    } catch (e) {
      state.error = e.message
      render()
    }
  })
  document.getElementById('btn-login-token')?.addEventListener('click', async () => {
    setToken(document.getElementById('login-token')?.value || '')
    try {
      await loadAll()
      state.authed = true
      state.authMode = 'token'
      state.page = 'overview'
      state.error = ''
      render()
    } catch (e) {
      state.error = e.message
      render()
    }
  })
  document.getElementById('btn-logout')?.addEventListener('click', async () => {
    try { await logout() } catch (_) {}
    clearToken()
    state.authed = false
    state.page = 'login'
    render()
  })
  document.getElementById('btn-save-budget')?.addEventListener('click', async () => {
    const amount = Number(document.getElementById('budget-amount')?.value || 0)
    const kind = document.getElementById('budget-kind')?.value || 'consume'
    const pv = document.getElementById('budget-person')?.value
    try {
      await upsertBudget({ month: currentMonthStr(), amount, kind, person_id: pv ? Number(pv) : null })
      state.budgets = (await fetchBudgets(currentMonthStr())).items || []
      state.notice = '预算已保存'
      render()
    } catch (e) { state.error = e.message; render() }
  })
  document.querySelectorAll('[data-del-budget]').forEach((btn) => btn.addEventListener('click', async () => {
    try {
      await deleteBudget(Number(btn.dataset.delBudget))
      state.budgets = (await fetchBudgets(currentMonthStr())).items || []
      render()
    } catch (e) { state.error = e.message; render() }
  }))
  document.getElementById('btn-add-rule')?.addEventListener('click', async () => {
    const match_field = document.getElementById('rule-field')?.value
    const match_op = document.getElementById('rule-op')?.value
    const match_value = document.getElementById('rule-value')?.value
    const pv = document.getElementById('rule-person')?.value
    const tv = document.getElementById('rule-tag')?.value
    try {
      await createRule({
        match_field, match_op, match_value,
        person_id: pv ? Number(pv) : null,
        tag_id: tv ? Number(tv) : null,
        name: match_value || '',
      })
      state.rules = (await fetchRules()).items || []
      state.notice = '规则已添加'
      render()
    } catch (e) { state.error = e.message; render() }
  })
  document.querySelectorAll('[data-del-rule]').forEach((btn) => btn.addEventListener('click', async () => {
    try {
      await deleteRule(Number(btn.dataset.delRule))
      state.rules = (await fetchRules()).items || []
      render()
    } catch (e) { state.error = e.message; render() }
  }))
  document.getElementById('btn-add-goal')?.addEventListener('click', async () => {
    try {
      await createGoal(document.getElementById('goal-name')?.value || '', Number(document.getElementById('goal-target')?.value || 0))
      state.goals = (await fetchGoals()).items || []
      render()
    } catch (e) { state.error = e.message; render() }
  })
  document.querySelectorAll('[data-del-goal]').forEach((btn) => btn.addEventListener('click', async () => {
    try {
      await deleteGoal(Number(btn.dataset.delGoal))
      state.goals = (await fetchGoals()).items || []
      render()
    } catch (e) { state.error = e.message; render() }
  }))
  document.getElementById('btn-export')?.addEventListener('click', (e) => {
    e.preventDefault()
    const from = document.getElementById('export-from')?.value || ''
    const to = document.getElementById('export-to')?.value || ''
    const url = exportCSVUrl(from, to)
    const token = getToken()
    const headers = {}
    if (token) headers.Authorization = 'Bearer ' + token
    fetch(url, { credentials: 'include', headers })
      .then((r) => {
        if (!r.ok) throw new Error('export failed')
        return r.blob()
      })
      .then((b) => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(b)
        a.download = 'cashpulse.csv'
        a.click()
      })
      .catch((err) => { state.error = err.message; render() })
  })

  document.querySelectorAll('[data-page]').forEach((el) => {
    el.addEventListener('click', () => {
      state.page = el.dataset.page
      state.notice = ''
      render()
      if (state.page === 'label') loadLabelQueue().then(() => render())
    })
  })

  document.getElementById('btn-refresh')?.addEventListener('click', () => loadAll())

  document.querySelectorAll('[data-period-mode]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const mode = btn.dataset.periodMode
      if (mode === 'preset') {
        state.periodMode = 'preset'
        state.periodPreset = Number(btn.dataset.preset)
      } else if (mode === 'month') {
        state.periodMode = 'month'
        if (!state.periodMonth) state.periodMonth = currentMonthStr()
      } else if (mode === 'custom') {
        state.periodMode = 'custom'
        if (!state.periodFrom || !state.periodTo) {
          // default last 30 days
          const end = new Date()
          const start = new Date()
          start.setDate(end.getDate() - 29)
          state.periodFrom = ymd(start)
          state.periodTo = ymd(end)
        }
      }
      if (mode !== 'custom') loadAll()
      else render()
    })
  })

  document.getElementById('period-month')?.addEventListener('change', (e) => {
    state.periodMonth = e.target.value
    state.periodMode = 'month'
    loadAll()
  })
  document.getElementById('btn-month-prev')?.addEventListener('click', () => {
    state.periodMonth = shiftMonth(state.periodMonth, -1)
    loadAll()
  })
  document.getElementById('btn-month-next')?.addEventListener('click', () => {
    state.periodMonth = shiftMonth(state.periodMonth, 1)
    loadAll()
  })
  document.getElementById('btn-month-now')?.addEventListener('click', () => {
    state.periodMonth = currentMonthStr()
    loadAll()
  })
  document.getElementById('btn-period-apply')?.addEventListener('click', () => {
    state.periodFrom = document.getElementById('period-from')?.value || ''
    state.periodTo = document.getElementById('period-to')?.value || ''
    if (!state.periodFrom || !state.periodTo) {
      state.error = '请选择起止日期'
      render()
      return
    }
    state.periodMode = 'custom'
    loadAll()
  })

  document.querySelectorAll('[data-kind]').forEach((btn) => {
    btn.addEventListener('click', () => {
      state.kindFilter = btn.dataset.kind
      loadAll()
    })
  })

  document.querySelectorAll('[data-label-window]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      state.labelWindow = btn.dataset.labelWindow
      await loadLabelQueue()
      render()
    })
  })

  document.getElementById('only-unlabeled')?.addEventListener('change', async (e) => {
    state.onlyUnlabeled = e.target.checked
    await loadLabelQueue()
    render()
  })

  document.querySelectorAll('[data-action]').forEach((btn) => {
    btn.addEventListener('click', () => onLabelClick(btn))
  })

  document.getElementById('btn-search')?.addEventListener('click', async () => {
    state.search = document.getElementById('search-q')?.value || ''
    try {
      const data = await fetchTransactions({ q: state.search, limit: 80 })
      state.transactions = data.items || []
      state.error = ''
      render()
    } catch (e) {
      state.error = e.message
      render()
    }
  })

  document.getElementById('btn-add-person')?.addEventListener('click', async () => {
    const name = document.getElementById('new-person')?.value || ''
    try {
      await createPerson(name)
      state.people = (await fetchPeople()).items || []
      state.notice = `已添加「${name.trim()}」`
      state.error = ''
      render()
    } catch (e) {
      state.error = e.message
      render()
    }
  })

  document.getElementById('btn-add-tag')?.addEventListener('click', async () => {
    const name = document.getElementById('new-tag')?.value || ''
    try {
      await createTag(name)
      state.tags = (await fetchTags()).items || []
      state.notice = `已添加标签「${name.trim()}」`
      state.error = ''
      render()
    } catch (e) {
      state.error = e.message
      render()
    }
  })

  document.querySelectorAll('[data-del-person]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!confirm('删除此人？其流水归属会清空。')) return
      try {
        await deletePerson(Number(btn.dataset.delPerson))
        await loadAll()
      } catch (e) {
        state.error = e.message
        render()
      }
    })
  })

  document.querySelectorAll('[data-del-tag]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!confirm('删除此标签？')) return
      try {
        await deleteTag(Number(btn.dataset.delTag))
        await loadAll()
      } catch (e) {
        state.error = e.message
        render()
      }
    })
  })

  document.getElementById('btn-save-token')?.addEventListener('click', () => {
    setToken(document.getElementById('token-input')?.value || '')
    state.error = ''
    state.notice = 'Token 已保存'
    loadAll()
  })

  document.getElementById('btn-clear-token')?.addEventListener('click', () => {
    clearToken()
    state.analytics = null
    state.transactions = []
    state.labelQueue = []
    state.notice = 'Token 已清除'
    render()
  })

  document.getElementById('btn-send-sms')?.addEventListener('click', async () => {
    const text = document.getElementById('test-sms')?.value || ''
    const out = document.getElementById('test-result')
    try {
      const data = await postSMS(text)
      if (out) out.textContent = JSON.stringify(data, null, 2)
      state.notice = '短信已入库'
      await loadAll()
    } catch (e) {
      if (out) out.textContent = e.message
    }
  })

  document.getElementById('btn-load-unparsed')?.addEventListener('click', async () => {
    try {
      state.unparsed = (await fetchUnparsed()).items || []
      state.notice = `未解析 ${state.unparsed.length} 条`
      render()
    } catch (e) {
      state.error = e.message
      render()
    }
  })
}

async function loadLabelQueue() {
  const { from, to } = labelDateRange()
  const data = await fetchTransactions({
    limit: 200,
    unlabeled: state.onlyUnlabeled,
    from,
    to,
  })
  state.labelQueue = data.items || []
}

async function loadAll() {
  if (!getToken()) {
    state.error = '请先设置 API Token'
    render()
    return
  }
  state.loading = true
  state.error = ''
  render()
  try {
    const [analytics, txns, people, tags, digest, budgets, rules, goals, cards] = await Promise.all([
      fetchAnalytics(analyticsQuery()),
      fetchTransactions({ q: state.search, limit: 80 }),
      fetchPeople(),
      fetchTags(),
      fetchDigest().catch(() => null),
      fetchBudgets(currentMonthStr()).catch(() => ({ items: [] })),
      fetchRules().catch(() => ({ items: [] })),
      fetchGoals().catch(() => ({ items: [] })),
      fetchCards().catch(() => ({ items: [] })),
    ])
    state.analytics = analytics
    state.transactions = txns.items || []
    state.people = people.items || []
    state.tags = tags.items || []
    state.digest = digest
    state.budgets = budgets.items || []
    state.rules = rules.items || []
    state.goals = goals.items || []
    state.cards = cards.items || []
    state.authed = true
    await loadLabelQueue()
  } catch (e) {
    state.error = e.status === 401 ? 'Token 无效' : e.message
  } finally {
    state.loading = false
    render()
  }
}

async function bootstrap() {
  render()
  try {
    const me = await fetchMe()
    state.passwordLogin = !!me.password_login
    state.authed = !!me.authenticated
    state.authMode = me.mode || 'none'
    if (state.authed) {
      await loadAll()
      return
    }
  } catch (e) {
    // offline or unconfigured
  }
  // allow legacy admin token in localStorage
  if (getToken()) {
    try {
      await loadAll()
      state.authed = true
      state.authMode = 'token'
      render()
      return
    } catch (_) {}
  }
  state.page = 'login'
  render()
}

bootstrap()
