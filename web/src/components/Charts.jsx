import {
  Chart as ChartJS,
  BarController,
  BarElement,
  LineController,
  LineElement,
  PointElement,
  ArcElement,
  DoughnutController,
  CategoryScale,
  LinearScale,
  Filler,
  Tooltip,
  Legend,
} from 'chart.js'
import { Bar, Doughnut, Line } from 'react-chartjs-2'
import { useMemo } from 'react'

ChartJS.register(
  BarController,
  BarElement,
  LineController,
  LineElement,
  PointElement,
  ArcElement,
  DoughnutController,
  CategoryScale,
  LinearScale,
  Filler,
  Tooltip,
  Legend,
)

function css(name, fallback) {
  if (typeof window === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

function theme() {
  return {
    text: css('--ink-2', '#4b5563'),
    muted: css('--ink-3', '#9ca3af'),
    grid: css('--line', '#e8eaed'),
    surface: css('--surface', '#ffffff'),
    brand: css('--brand', '#2563eb'),
    expense: css('--expense', '#dc2626'),
    income: css('--income', '#059669'),
  }
}

function moneyTick(v) {
  const n = Number(v) || 0
  if (Math.abs(n) >= 10000) return `¥${(n / 10000).toFixed(n % 10000 === 0 ? 0 : 1)}万`
  if (Math.abs(n) >= 1000) return `¥${(n / 1000).toFixed(n % 1000 === 0 ? 0 : 1)}k`
  return `¥${n.toFixed(0)}`
}

function moneyFull(v) {
  return `¥${(Number(v) || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

export function ChartsDaily({ daily = [], height = 260 }) {
  const t = theme()
  let items = [...daily]
  if (items.length > 60) {
    while (items.length > 1 && !(items[0].expense > 0) && !(items[0].txn_count > 0)) items.shift()
  }
  if (!items.some((d) => d.expense > 0)) {
    return <div className="empty-chart">这段时间还没有消费金额</div>
  }
  const labels = items.map((d) => {
    const p = d.date.split('-')
    return `${Number(p[1])}/${Number(p[2])}`
  })
  const data = items.map((d) => d.expense || 0)
  const tickLimit = items.length <= 7 ? 7 : items.length <= 15 ? 15 : items.length <= 31 ? 16 : 10

  return (
    <div className="chart-box" style={{ height }}>
      <Bar
        data={{
          labels,
          datasets: [{
            label: '支出金额',
            data,
            backgroundColor: t.brand,
            borderRadius: { topLeft: 4, topRight: 4 },
            maxBarThickness: items.length <= 15 ? 28 : 20,
          }],
        }}
        options={{
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { display: false },
            tooltip: {
              callbacks: {
                label: (ctx) => {
                  const row = items[ctx.dataIndex]
                  return ` 花了 ${moneyFull(ctx.parsed.y)}（${row?.txn_count || 0} 笔）`
                },
              },
            },
          },
          scales: {
            x: {
              grid: { display: false },
              ticks: { color: t.muted, maxTicksLimit: tickLimit, autoSkip: items.length > 31, maxRotation: 0 },
            },
            y: {
              beginAtZero: true,
              grid: { color: t.grid },
              title: { display: true, text: '金额', color: t.muted, font: { size: 11 } },
              ticks: { color: t.muted, callback: moneyTick, maxTicksLimit: 5 },
            },
          },
        }}
      />
    </div>
  )
}

export function ChartsDonut({ rows = [], height = 280 }) {
  const t = theme()
  let items = rows.filter((r) => (r.expense || 0) > 0)
  if (!items.length) return <div className="empty-chart">暂无渠道数据</div>
  if (items.length > 7) {
    const head = items.slice(0, 6)
    const rest = items.slice(6)
    const other = rest.reduce((a, r) => ({ expense: a.expense + (r.expense || 0), name: '其他' }), { expense: 0, name: '其他' })
    items = [...head, other]
  }
  const palette = [t.brand, t.income, '#db2777', '#d97706', '#0d9488', '#ea580c', '#7c3aed', '#94a3b8']
  return (
    <div className="chart-box" style={{ height }}>
      <Doughnut
        data={{
          labels: items.map((r) => r.name),
          datasets: [{
            data: items.map((r) => r.expense || 0),
            backgroundColor: items.map((_, i) => palette[i % palette.length]),
            borderWidth: 2,
            borderColor: t.surface,
          }],
        }}
        options={{
          responsive: true,
          maintainAspectRatio: false,
          cutout: '62%',
          plugins: {
            legend: { position: 'right', labels: { color: t.text, boxWidth: 10, font: { size: 11 } } },
            tooltip: {
              callbacks: {
                label: (ctx) => {
                  const total = ctx.dataset.data.reduce((a, b) => a + b, 0) || 1
                  const pct = ((ctx.parsed / total) * 100).toFixed(1)
                  return ` ${ctx.label}: ${moneyFull(ctx.parsed)} (${pct}%)`
                },
              },
            },
          },
        }}
      />
    </div>
  )
}

export function ChartsHBar({ rows = [], nameKey = 'name', height = 260 }) {
  const t = theme()
  const items = rows.filter((r) => (r.expense || 0) > 0).slice(0, 10)
  if (!items.length) return <div className="empty-chart">暂无数据</div>
  const palette = [t.brand, t.income, '#db2777', '#d97706', '#0d9488', '#ea580c', '#7c3aed', '#e11d48']
  return (
    <div className="chart-box" style={{ height }}>
      <Bar
        data={{
          labels: items.map((r) => r[nameKey] || r.name || r.person_name || '—'),
          datasets: [{
            label: '支出金额',
            data: items.map((r) => r.expense || 0),
            backgroundColor: items.map((_, i) => palette[i % palette.length]),
            borderRadius: 4,
            maxBarThickness: 16,
          }],
        }}
        options={{
          indexAxis: 'y',
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { display: false },
            tooltip: {
              callbacks: {
                label: (ctx) => {
                  const row = items[ctx.dataIndex]
                  return ` ${moneyFull(ctx.parsed.x)} · ${row?.txn_count || 0} 笔`
                },
              },
            },
          },
          scales: {
            x: {
              beginAtZero: true,
              grid: { color: t.grid },
              ticks: { color: t.muted, callback: moneyTick, maxTicksLimit: 5 },
            },
            y: { grid: { display: false }, ticks: { color: t.text, font: { size: 12 } } },
          },
        }}
      />
    </div>
  )
}

export function ChartsMonthly({ monthly = [], height = 260 }) {
  const t = theme()
  const items = monthly.filter((m) => (m.expense || 0) > 0 || (m.income || 0) > 0)
  if (!items.length) return <div className="empty-chart">暂无月度数据</div>
  const labels = items.map((m) => {
    const p = m.month.split('-')
    return `${p[0].slice(2)}/${Number(p[1])}`
  })
  return (
    <div className="chart-box" style={{ height }}>
      <Bar
        data={{
          labels,
          datasets: [
            { label: '支出', data: items.map((m) => m.expense || 0), backgroundColor: t.expense, maxBarThickness: 14 },
            { label: '收入', data: items.map((m) => m.income || 0), backgroundColor: t.income, maxBarThickness: 14 },
          ],
        }}
        options={{
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { position: 'top', align: 'end', labels: { boxWidth: 10, font: { size: 11 } } },
            tooltip: { callbacks: { label: (ctx) => ` ${ctx.dataset.label} ${moneyFull(ctx.parsed.y)}` } },
          },
          scales: {
            x: { grid: { display: false }, ticks: { color: t.muted, maxRotation: 0 } },
            y: { beginAtZero: true, grid: { color: t.grid }, ticks: { color: t.muted, callback: moneyTick } },
          },
        }}
      />
    </div>
  )
}

export function ChartsBalance({ series = [], height = 260 }) {
  const t = theme()
  let pts = [...series]
  if (pts.length < 2) return <div className="empty-chart">余额点不足</div>
  if (pts.length > 60) {
    const out = []
    const step = (pts.length - 1) / 59
    for (let i = 0; i < 60; i++) out.push(pts[Math.round(i * step)])
    pts = out
  }
  const labels = pts.map((p) => {
    const d = new Date(p.at)
    return Number.isNaN(d.getTime()) ? '' : `${d.getMonth() + 1}/${d.getDate()}`
  })
  const data = pts.map((p) => p.balance)
  const min = Math.min(...data)
  const max = Math.max(...data)
  const pad = Math.max(1, (max - min) * 0.08)
  const yMin = Math.max(0, min - pad)

  return (
    <div className="chart-box" style={{ height }}>
      <Line
        data={{
          labels,
          datasets: [{
            label: '账户余额',
            data,
            borderColor: t.brand,
            backgroundColor: 'rgba(37,99,235,0.12)',
            fill: true,
            tension: 0.15,
            pointRadius: 0,
            pointHoverRadius: 4,
            borderWidth: 2,
          }],
        }}
        options={{
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { display: false },
            tooltip: {
              callbacks: {
                title: (items) => {
                  const i = items[0]?.dataIndex ?? 0
                  const raw = pts[i]?.at
                  const d = raw ? new Date(raw) : null
                  return d && !Number.isNaN(d.getTime())
                    ? d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
                    : ''
                },
                label: (ctx) => ` 余额 ${moneyFull(ctx.parsed.y)}`,
              },
            },
          },
          scales: {
            x: { grid: { display: false }, ticks: { color: t.muted, maxTicksLimit: 8, maxRotation: 0 } },
            y: {
              min: yMin,
              grid: { color: t.grid },
              title: { display: true, text: '余额', color: t.muted, font: { size: 11 } },
              ticks: { color: t.muted, callback: moneyTick, maxTicksLimit: 5 },
            },
          },
        }}
      />
    </div>
  )
}
