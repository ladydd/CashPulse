import {
  Chart,
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

Chart.register(
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

/** @type {Map<string, Chart>} */
const charts = new Map()

function cssVar(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

function theme() {
  return {
    text: cssVar('--text-2', '#4b5563'),
    textMuted: cssVar('--text-3', '#9ca3af'),
    grid: cssVar('--line', '#e8eaed'),
    surface: cssVar('--bg-elev', '#ffffff'),
    brand: cssVar('--brand', '#2563eb'),
    expense: cssVar('--expense', '#dc2626'),
    income: cssVar('--income', '#059669'),
  }
}

function moneyTick(v) {
  const n = Number(v) || 0
  if (Math.abs(n) >= 10000) return `¥${(n / 10000).toFixed(n % 10000 === 0 ? 0 : 1)}万`
  if (Math.abs(n) >= 1000) return `¥${(n / 1000).toFixed(n % 1000 === 0 ? 0 : 1)}k`
  return `¥${n.toFixed(0)}`
}

function moneyFull(v) {
  return `¥${(Number(v) || 0).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

function destroyAll() {
  for (const c of charts.values()) c.destroy()
  charts.clear()
}

function baseOptions(t) {
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index', intersect: false },
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: t.text,
        titleColor: t.surface,
        bodyColor: t.surface,
        padding: 10,
        cornerRadius: 8,
        displayColors: true,
        callbacks: {},
      },
    },
    scales: {
      x: {
        grid: { display: false },
        border: { display: false },
        ticks: {
          color: t.textMuted,
          maxRotation: 0,
          autoSkip: true,
          maxTicksLimit: 8,
          font: { size: 11 },
        },
      },
      y: {
        beginAtZero: true,
        border: { display: false },
        grid: {
          color: t.grid,
          drawTicks: false,
        },
        ticks: {
          color: t.textMuted,
          font: { size: 11 },
          padding: 8,
          callback: moneyTick,
          maxTicksLimit: 5,
        },
      },
    },
  }
}

/**
 * Daily expense columns.
 * @param {string} canvasId
 * @param {{date:string,expense:number,txn_count?:number}[]} daily
 */
export function mountDailyExpense(canvasId, daily) {
  const el = document.getElementById(canvasId)
  if (!el) return

  // Keep continuous calendar days for short ranges (7/15/30) so empty days still show.
  // Only for very long series, drop pure leading zeros to reduce noise — never drop
  // interior zeros (that would break "最近 N 天" as a continuous table).
  let items = [...(daily || [])]
  if (items.length > 60) {
    while (items.length > 1 && !(items[0].expense > 0) && !(items[0].txn_count > 0)) {
      items.shift()
    }
  }

  const t = theme()
  const labels = items.map((d) => {
    const p = d.date.split('-')
    return `${Number(p[1])}/${Number(p[2])}`
  })
  const data = items.map((d) => d.expense || 0)

  const prev = charts.get(canvasId)
  if (prev) prev.destroy()

  const tickLimit = items.length <= 7 ? 7 : items.length <= 15 ? 15 : items.length <= 31 ? 16 : 10
  const opts = baseOptions(t)
  opts.scales.x.ticks.maxTicksLimit = tickLimit
  opts.scales.x.ticks.autoSkip = items.length > 31

  const chart = new Chart(el, {
    type: 'bar',
    data: {
      labels,
      datasets: [
        {
          label: '支出',
          data,
          backgroundColor: t.brand,
          borderRadius: { topLeft: 4, topRight: 4, bottomLeft: 0, bottomRight: 0 },
          borderSkipped: false,
          maxBarThickness: items.length <= 15 ? 28 : 22,
          categoryPercentage: 0.75,
          barPercentage: 0.85,
        },
      ],
    },
    options: {
      ...opts,
      plugins: {
        ...opts.plugins,
        tooltip: {
          ...opts.plugins.tooltip,
          callbacks: {
            title: (items) => items[0]?.label || '',
            label: (ctx) => {
              const row = items[ctx.dataIndex]
              return ` 花了 ${moneyFull(ctx.parsed.y)}（${row?.txn_count || 0} 笔）`
            },
          },
        },
      },
    },
  })
  charts.set(canvasId, chart)
}

/**
 * Monthly expense vs income grouped bars.
 */
export function mountMonthly(canvasId, monthly) {
  const el = document.getElementById(canvasId)
  if (!el) return
  const items = (monthly || []).filter((m) => (m.expense || 0) > 0 || (m.income || 0) > 0)
  const t = theme()
  const labels = items.map((m) => {
    const p = m.month.split('-')
    return `${p[0].slice(2)}/${Number(p[1])}`
  })

  const prev = charts.get(canvasId)
  if (prev) prev.destroy()

  const chart = new Chart(el, {
    type: 'bar',
    data: {
      labels,
      datasets: [
        {
          label: '支出',
          data: items.map((m) => m.expense || 0),
          backgroundColor: t.expense,
          borderRadius: { topLeft: 3, topRight: 3, bottomLeft: 0, bottomRight: 0 },
          borderSkipped: false,
          maxBarThickness: 14,
        },
        {
          label: '收入',
          data: items.map((m) => m.income || 0),
          backgroundColor: t.income,
          borderRadius: { topLeft: 3, topRight: 3, bottomLeft: 0, bottomRight: 0 },
          borderSkipped: false,
          maxBarThickness: 14,
        },
      ],
    },
    options: {
      ...baseOptions(t),
      plugins: {
        legend: {
          display: true,
          position: 'top',
          align: 'end',
          labels: {
            color: t.text,
            boxWidth: 10,
            boxHeight: 10,
            borderRadius: 2,
            useBorderRadius: true,
            font: { size: 11 },
            padding: 12,
          },
        },
        tooltip: {
          ...baseOptions(t).plugins.tooltip,
          callbacks: {
            label: (ctx) => ` ${ctx.dataset.label} ${moneyFull(ctx.parsed.y)}`,
          },
        },
      },
    },
  })
  charts.set(canvasId, chart)
}

/**
 * Account balance over time — line with clear axes, not an "ECG".
 * Downsample dense series for readability.
 */
export function mountBalance(canvasId, series) {
  const el = document.getElementById(canvasId)
  if (!el) return

  let pts = [...(series || [])]
  // downsample to ~60 points keeping first/last
  if (pts.length > 60) {
    const out = []
    const step = (pts.length - 1) / 59
    for (let i = 0; i < 60; i++) {
      out.push(pts[Math.round(i * step)])
    }
    pts = out
  }

  const t = theme()
  const labels = pts.map((p) => {
    const d = new Date(p.at)
    if (Number.isNaN(d.getTime())) return ''
    return `${d.getMonth() + 1}/${d.getDate()}`
  })
  const data = pts.map((p) => p.balance)

  const prev = charts.get(canvasId)
  if (prev) prev.destroy()

  // y axis: pad a bit so line isn't glued to edges
  const min = Math.min(...data)
  const max = Math.max(...data)
  const pad = Math.max(1, (max - min) * 0.08)
  // balance shouldn't show a nonsense negative floor
  const yMin = Math.max(0, min - pad)
  const yMax = max + pad

  const chart = new Chart(el, {
    type: 'line',
    data: {
      labels,
      datasets: [
        {
          label: '账户余额',
          data,
          borderColor: t.brand,
          backgroundColor: hexAlpha(t.brand, 0.12),
          borderWidth: 2,
          fill: true,
          tension: 0.15,
          pointRadius: 0,
          pointHoverRadius: 5,
          pointHoverBackgroundColor: t.brand,
          pointHoverBorderColor: t.surface,
          pointHoverBorderWidth: 2,
        },
      ],
    },
    options: {
      ...baseOptions(t),
      plugins: {
        legend: { display: false },
        tooltip: {
          ...baseOptions(t).plugins.tooltip,
          callbacks: {
            title: (items) => {
              const i = items[0]?.dataIndex ?? 0
              const raw = pts[i]?.at
              const d = raw ? new Date(raw) : null
              if (!d || Number.isNaN(d.getTime())) return items[0]?.label || ''
              return d.toLocaleString('zh-CN', {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
              })
            },
            label: (ctx) => ` 余额 ${moneyFull(ctx.parsed.y)}`,
          },
        },
      },
      scales: {
        ...baseOptions(t).scales,
        y: {
          ...baseOptions(t).scales.y,
          beginAtZero: yMin === 0,
          min: yMin,
          max: yMax,
        },
      },
    },
  })
  charts.set(canvasId, chart)
}

/**
 * Horizontal bar: channel / person share.
 */
export function mountHBar(canvasId, rows, nameKey = 'name') {
  const el = document.getElementById(canvasId)
  if (!el) return
  const items = (rows || []).filter((r) => (r.expense || 0) > 0).slice(0, 10)
  const t = theme()
  const palette = [t.brand, t.income, '#db2777', '#d97706', '#0d9488', '#ea580c', '#7c3aed', '#e11d48', '#64748b', '#0891b2']

  const prev = charts.get(canvasId)
  if (prev) prev.destroy()

  const chart = new Chart(el, {
    type: 'bar',
    data: {
      labels: items.map((r) => r[nameKey] || r.name || r.person_name || '—'),
      datasets: [
        {
          label: '支出',
          data: items.map((r) => r.expense || 0),
          backgroundColor: items.map((_, i) => palette[i % palette.length]),
          borderRadius: 4,
          borderSkipped: false,
          maxBarThickness: 16,
        },
      ],
    },
    options: {
      indexAxis: 'y',
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: t.text,
          titleColor: t.surface,
          bodyColor: t.surface,
          padding: 10,
          cornerRadius: 8,
          callbacks: {
            label: (ctx) => {
              const row = items[ctx.dataIndex]
              const pct = row?.pct != null ? ` · ${row.pct}%` : ''
              return ` ${moneyFull(ctx.parsed.x)} · ${row?.txn_count || 0} 笔${pct}`
            },
          },
        },
      },
      scales: {
        x: {
          beginAtZero: true,
          border: { display: false },
          grid: { color: t.grid, drawTicks: false },
          ticks: {
            color: t.textMuted,
            font: { size: 11 },
            callback: moneyTick,
            maxTicksLimit: 5,
          },
        },
        y: {
          border: { display: false },
          grid: { display: false },
          ticks: {
            color: t.text,
            font: { size: 12 },
          },
        },
      },
    },
  })
  charts.set(canvasId, chart)
}

function hexAlpha(color, a) {
  // support #rgb/#rrggbb or fall back to color-mix style via rgba if not hex
  if (color.startsWith('#') && color.length === 7) {
    const r = parseInt(color.slice(1, 3), 16)
    const g = parseInt(color.slice(3, 5), 16)
    const b = parseInt(color.slice(5, 7), 16)
    return `rgba(${r},${g},${b},${a})`
  }
  return color
}

/**
 * Doughnut composition (channel / category).
 */
export function mountDoughnut(canvasId, rows, nameKey = 'name') {
  const el = document.getElementById(canvasId)
  if (!el) return
  let items = (rows || []).filter((r) => (r.expense || 0) > 0)
  // fold long tail into 其他
  if (items.length > 7) {
    const head = items.slice(0, 6)
    const rest = items.slice(6)
    const other = rest.reduce(
      (a, r) => {
        a.expense += r.expense || 0
        a.txn_count += r.txn_count || 0
        return a
      },
      { [nameKey]: '其他', expense: 0, txn_count: 0 },
    )
    items = [...head, other]
  }
  const t = theme()
  const palette = [t.brand, t.income, '#db2777', '#d97706', '#0d9488', '#ea580c', '#7c3aed', '#94a3b8']

  const prev = charts.get(canvasId)
  if (prev) prev.destroy()

  const chart = new Chart(el, {
    type: 'doughnut',
    data: {
      labels: items.map((r) => r[nameKey] || r.name || '—'),
      datasets: [
        {
          data: items.map((r) => r.expense || 0),
          backgroundColor: items.map((_, i) => palette[i % palette.length]),
          borderWidth: 2,
          borderColor: t.surface,
          hoverOffset: 4,
        },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      cutout: '62%',
      plugins: {
        legend: {
          position: 'right',
          labels: {
            color: t.text,
            boxWidth: 10,
            boxHeight: 10,
            borderRadius: 2,
            useBorderRadius: true,
            font: { size: 11 },
            padding: 10,
          },
        },
        tooltip: {
          backgroundColor: t.text,
          titleColor: t.surface,
          bodyColor: t.surface,
          padding: 10,
          cornerRadius: 8,
          callbacks: {
            label: (ctx) => {
              const total = ctx.dataset.data.reduce((a, b) => a + b, 0) || 1
              const v = ctx.parsed
              const pct = ((v / total) * 100).toFixed(1)
              return ` ${ctx.label}: ${moneyFull(v)} (${pct}%)`
            },
          },
        },
      },
    },
  })
  charts.set(canvasId, chart)
}

export function teardownCharts() {
  destroyAll()
}
