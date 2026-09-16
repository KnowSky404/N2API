/** @param {string | null | undefined} value */
export function requestLogErrorLabel(value) {
  if (!value) return '-';

  return value
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

/** @param {number | null | undefined} value */
export function requestLogDiagnosticDurationLabel(value) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? `${value}ms` : '未记录';
}

/** @param {string | null | undefined} value */
export function requestLogAttemptTypeLabel(value) {
  /** @type {Record<string, string>} */
  const labels = {
    selection: 'Account selection',
    concurrency_rejection: 'Concurrency rejection',
    gateway_rejection: 'Gateway rejection',
    upstream_http: 'Upstream HTTP',
    upstream_transport: 'Upstream transport',
    auth_refresh_retry: 'Auth refresh retry'
  };
  return value ? labels[value] ?? requestLogErrorLabel(value) : '未记录';
}

/** @param {string | null | undefined} value */
export function requestLogAttemptTimestampLabel(value) {
  if (!value || !Number.isFinite(Date.parse(value))) return '未记录';
  return new Date(value).toLocaleString();
}

/** @param {{ accountName?: string | null, accountId?: number | null }} attempt */
export function requestLogAttemptAccountLabel(attempt) {
  if (attempt.accountName && attempt.accountId) return `${attempt.accountName} (#${attempt.accountId})`;
  if (attempt.accountName) return attempt.accountName;
  if (attempt.accountId) return `Account ${attempt.accountId}`;
  return '未记录';
}
