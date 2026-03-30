export class HttpError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'HttpError'
    this.status = status
  }
}

type ApiEnvelope<T> = {
  data: T
}

export function createApi(token: string, onUnauthorized?: () => void) {
  return {
    get: <T>(path: string) => request<T>(path, { method: 'GET' }, token, onUnauthorized),
    post: <T>(path: string, body?: unknown) =>
      request<T>(
        path,
        {
          method: 'POST',
          body: body === undefined ? undefined : JSON.stringify(body),
        },
        token,
        onUnauthorized,
      ),
    put: <T>(path: string, body?: unknown) =>
      request<T>(
        path,
        {
          method: 'PUT',
          body: body === undefined ? undefined : JSON.stringify(body),
        },
        token,
        onUnauthorized,
      ),
    delete: (path: string) =>
      request<void>(path, { method: 'DELETE' }, token, onUnauthorized),
  }
}

export function postPublic<T>(path: string, body: unknown) {
  return request<T>(
    path,
    {
      method: 'POST',
      body: JSON.stringify(body),
    },
    undefined,
    undefined,
  )
}

async function request<T>(
  path: string,
  init: RequestInit,
  token?: string,
  onUnauthorized?: () => void,
) {
  const baseUrls = resolveApiBaseUrls()
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body) {
    headers.set('Content-Type', 'application/json')
  }
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const failures: string[] = []

  for (let index = 0; index < baseUrls.length; index += 1) {
    const baseUrl = baseUrls[index]
    const hasMoreCandidates = index < baseUrls.length - 1

    let response: Response

    try {
      response = await fetch(`${baseUrl}${path}`, {
        ...init,
        headers,
      })
    } catch (error) {
      failures.push(`${baseUrl}: ${describeFailure(error)}`)
      continue
    }

    if (response.status === 401 && onUnauthorized) {
      onUnauthorized()
    }

    const text = await response.text()
    const parsed = text ? safeParse(text) : null

    if (!response.ok) {
      const message =
        typeof parsed === 'object' && parsed && 'message' in parsed
          ? String(parsed.message)
          : response.statusText || 'Request failed'

      if (hasMoreCandidates && shouldTryNextBase(response.status)) {
        failures.push(`${baseUrl}: HTTP ${response.status} ${message}`)
        continue
      }

      throw new HttpError(message, response.status)
    }

    if (!text) {
      return undefined as T
    }

    if (parsed && typeof parsed === 'object' && 'data' in parsed) {
      return (parsed as ApiEnvelope<T>).data
    }

    return parsed as T
  }

  throw new HttpError(buildFetchFailureMessage(path, baseUrls, failures), 0)
}

function safeParse(value: string) {
  try {
    return JSON.parse(value)
  } catch {
    return value
  }
}

export function resolveApiBaseUrls() {
  const candidates: string[] = []
  const envBase = import.meta.env.VITE_API_BASE_URL?.trim()
  if (envBase) {
    candidates.push(trimTrailingSlash(envBase))
  }

  if (typeof window !== 'undefined') {
    const protocol = window.location.protocol
    if (protocol === 'http:' || protocol === 'https:') {
      candidates.push(`${trimTrailingSlash(window.location.origin)}/api`)
    }
  }

  candidates.push('http://127.0.0.1:18081/api')
  candidates.push('http://localhost:18081/api')

  return candidates.filter((value, index, all) => all.indexOf(value) === index)
}

export function trimTrailingSlash(value: string) {
  return value.endsWith('/') ? value.slice(0, -1) : value
}

function buildFetchFailureMessage(path: string, baseUrls: string[], failures: string[]) {
  const locationHint =
    typeof window !== 'undefined' ? `当前页面：${window.location.href}` : '当前页面：unknown'
  const baseHint = baseUrls.join(' , ')
  const detail = failures.length > 0 ? failures.join(' ; ') : 'network error'
  return `无法连接到 API。请求路径：${path}。已尝试地址：${baseHint}。${locationHint}。请确认后端已启动，或通过 VITE_API_BASE_URL / VITE_PROXY_TARGET 指向正确地址。原始错误：${detail}`
}

function shouldTryNextBase(status: number) {
  return [404, 405, 502, 503, 504].includes(status)
}

function describeFailure(error: unknown) {
  return error instanceof Error ? error.message : 'network error'
}
