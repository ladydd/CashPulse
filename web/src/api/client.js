const TOKEN_KEY = 'cashpulse_api_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(token) {
  if (!token?.trim()) localStorage.removeItem(TOKEN_KEY)
  else localStorage.setItem(TOKEN_KEY, token.trim())
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

async function request(path, options = {}) {
  const headers = {
    Accept: 'application/json',
    ...(options.headers || {}),
  }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (options.body && !headers['Content-Type']) headers['Content-Type'] = 'application/json'

  const res = await fetch(path, { credentials: 'include', ...options, headers })
  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = { error: text || 'invalid response' }
  }
  if (!res.ok) {
    const err = new Error(data?.error || res.statusText || 'request failed')
    err.status = res.status
    throw err
  }
  return data
}

export const api = {
  me: () => request('/api/v1/auth/me'),
  login: (password) => request('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ password }) }),
  logout: () => request('/api/v1/auth/logout', { method: 'POST' }),
  bootstrap: (opts = {}) => {
    const p = new URLSearchParams()
    if (opts.from && opts.to) {
      p.set('from', opts.from)
      p.set('to', opts.to)
    } else if (opts.month) p.set('month', opts.month)
    else if (opts.days === 0 || opts.days === 'all') p.set('days', 'all')
    else if (opts.days != null) p.set('days', String(opts.days))
    else p.set('days', '30')
    p.set('kind', opts.kind || 'consume')
    if (opts.month) p.set('month', opts.month)
    if (opts.limit) p.set('limit', String(opts.limit))
    return request(`/api/v1/bootstrap?${p}`)
  },
  analytics: (opts = {}) => {
    const p = new URLSearchParams()
    if (opts.from && opts.to) {
      p.set('from', opts.from)
      p.set('to', opts.to)
    } else if (opts.month) p.set('month', opts.month)
    else if (opts.days === 0 || opts.days === 'all') p.set('days', 'all')
    else p.set('days', String(opts.days ?? 30))
    p.set('kind', opts.kind || 'consume')
    return request(`/api/v1/analytics?${p}`)
  },
  transactions: ({ q = '', limit = 80, offset = 0, unlabeled = false, from = '', to = '' } = {}) => {
    const p = new URLSearchParams({ limit: String(limit), offset: String(offset) })
    if (q) p.set('q', q)
    if (unlabeled) p.set('unlabeled', '1')
    if (from) p.set('from', from)
    if (to) p.set('to', to)
    return request(`/api/v1/transactions?${p}`)
  },
  label: (id, body) => request(`/api/v1/transactions/${id}/labels`, { method: 'PATCH', body: JSON.stringify(body) }),
  people: () => request('/api/v1/people'),
  createPerson: (name) => request('/api/v1/people', { method: 'POST', body: JSON.stringify({ name }) }),
  deletePerson: (id) => request(`/api/v1/people/${id}`, { method: 'DELETE' }),
  tags: () => request('/api/v1/tags'),
  createTag: (name) => request('/api/v1/tags', { method: 'POST', body: JSON.stringify({ name }) }),
  deleteTag: (id) => request(`/api/v1/tags/${id}`, { method: 'DELETE' }),
  digest: () => request('/api/v1/digest'),
  budgets: (month = '') => request(`/api/v1/budgets${month ? `?month=${month}` : ''}`),
  upsertBudget: (body) => request('/api/v1/budgets', { method: 'PUT', body: JSON.stringify(body) }),
  deleteBudget: (id) => request(`/api/v1/budgets/${id}`, { method: 'DELETE' }),
  rules: () => request('/api/v1/rules'),
  createRule: (body) => request('/api/v1/rules', { method: 'POST', body: JSON.stringify(body) }),
  deleteRule: (id) => request(`/api/v1/rules/${id}`, { method: 'DELETE' }),
  goals: () => request('/api/v1/goals'),
  createGoal: (name, target) => request('/api/v1/goals', { method: 'POST', body: JSON.stringify({ name, target }) }),
  deleteGoal: (id) => request(`/api/v1/goals/${id}`, { method: 'DELETE' }),
  cards: () => request('/api/v1/cards'),
  unparsed: () => request('/api/v1/unparsed'),
  postSMS: (text) => request('/api/v1/sms', { method: 'POST', body: JSON.stringify({ text, source: 'web-test' }) }),
  exportUrl: (from, to) => {
    const p = new URLSearchParams()
    if (from) p.set('from', from)
    if (to) p.set('to', to)
    return `/api/v1/export/transactions.csv?${p}`
  },
}
