const TOKEN_KEY = 'cashpulse_api_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(token) {
  if (!token || !String(token).trim()) {
    localStorage.removeItem(TOKEN_KEY)
    return
  }
  localStorage.setItem(TOKEN_KEY, token.trim())
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

export function fetchMe() {
  return request('/api/v1/auth/me')
}

export function login(password) {
  return request('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export function logout() {
  return request('/api/v1/auth/logout', { method: 'POST' })
}

export function fetchDigest() {
  return request('/api/v1/digest')
}

export function fetchBudgets(month = '') {
  const q = month ? `?month=${encodeURIComponent(month)}` : ''
  return request(`/api/v1/budgets${q}`)
}

export function upsertBudget(body) {
  return request('/api/v1/budgets', { method: 'PUT', body: JSON.stringify(body) })
}

export function deleteBudget(id) {
  return request(`/api/v1/budgets/${id}`, { method: 'DELETE' })
}

export function fetchRules() {
  return request('/api/v1/rules')
}

export function createRule(body) {
  return request('/api/v1/rules', { method: 'POST', body: JSON.stringify(body) })
}

export function deleteRule(id) {
  return request(`/api/v1/rules/${id}`, { method: 'DELETE' })
}

export function fetchGoals() {
  return request('/api/v1/goals')
}

export function createGoal(name, target) {
  return request('/api/v1/goals', { method: 'POST', body: JSON.stringify({ name, target }) })
}

export function deleteGoal(id) {
  return request(`/api/v1/goals/${id}`, { method: 'DELETE' })
}

export function fetchCards() {
  return request('/api/v1/cards')
}

export function exportCSVUrl(from, to) {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  return `/api/v1/export/transactions.csv?${params}`
}

async function request(path, options = {}) {
  const token = getToken()
  const headers = {
    Accept: 'application/json',
    ...(options.headers || {}),
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  if (options.body && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
  }

  const res = await fetch(path, { credentials: 'include', ...options, headers })
  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = { error: text || 'invalid response' }
  }

  if (!res.ok) {
    const msg = data?.error || res.statusText || 'request failed'
    const err = new Error(msg)
    err.status = res.status
    throw err
  }
  return data
}

/** @param {{days?: number|string, from?: string, to?: string, month?: string, kind?: string}} opts */
export function fetchAnalytics(opts = {}) {
  const params = new URLSearchParams()
  if (opts.from && opts.to) {
    params.set('from', opts.from)
    params.set('to', opts.to)
  } else if (opts.month) {
    params.set('month', opts.month)
  } else if (opts.days === 0 || opts.days === 'all') {
    params.set('days', 'all')
  } else if (opts.days != null) {
    params.set('days', String(opts.days))
  } else {
    params.set('days', '30')
  }
  params.set('kind', opts.kind || 'consume')
  return request(`/api/v1/analytics?${params}`)
}

export function fetchTransactions({
  q = '',
  limit = 80,
  unlabeled = false,
  personId = null,
  from = '',
  to = '',
} = {}) {
  const params = new URLSearchParams({ limit: String(limit) })
  if (q) params.set('q', q)
  if (unlabeled) params.set('unlabeled', '1')
  if (personId != null) params.set('person_id', String(personId))
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  return request(`/api/v1/transactions?${params}`)
}

export function fetchUnparsed(limit = 50) {
  return request(`/api/v1/unparsed?limit=${limit}`)
}

export function postSMS(text, source = 'web-test') {
  return request('/api/v1/sms', {
    method: 'POST',
    body: JSON.stringify({ text, source }),
  })
}

export function fetchPeople() {
  return request('/api/v1/people')
}

export function createPerson(name, color = '') {
  return request('/api/v1/people', {
    method: 'POST',
    body: JSON.stringify({ name, color }),
  })
}

export function deletePerson(id) {
  return request(`/api/v1/people/${id}`, { method: 'DELETE' })
}

export function fetchTags() {
  return request('/api/v1/tags')
}

export function createTag(name, color = '') {
  return request('/api/v1/tags', {
    method: 'POST',
    body: JSON.stringify({ name, color }),
  })
}

export function deleteTag(id) {
  return request(`/api/v1/tags/${id}`, { method: 'DELETE' })
}

export function labelTransaction(id, body) {
  return request(`/api/v1/transactions/${id}/labels`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function bulkLabel(body) {
  return request('/api/v1/labels/bulk', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function fetchUnlabeledMerchants(limit = 40) {
  return request(`/api/v1/labels/unlabeled-merchants?limit=${limit}`)
}
