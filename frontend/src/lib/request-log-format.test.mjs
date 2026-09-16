import { describe, expect, test } from 'bun:test';
import {
  requestLogAttemptAccountLabel,
  requestLogAttemptTimestampLabel,
  requestLogAttemptTypeLabel,
  requestLogDiagnosticDurationLabel,
  requestLogErrorLabel
} from './request-log-format.js';

describe('request log formatters', () => {
  test('formats stable diagnostic labels and preserves unknown codes', () => {
    expect(requestLogErrorLabel('upstream_model_error')).toBe('Upstream Model Error');
    expect(requestLogAttemptTypeLabel('upstream_transport')).toBe('Upstream transport');
    expect(requestLogAttemptTypeLabel('new_code')).toBe('New Code');
  });

  test('renders missing or invalid diagnostic values as unrecorded', () => {
    expect(requestLogDiagnosticDurationLabel(null)).toBe('未记录');
    expect(requestLogDiagnosticDurationLabel(-1)).toBe('未记录');
    expect(requestLogAttemptTimestampLabel('not-a-date')).toBe('未记录');
    expect(requestLogAttemptAccountLabel({})).toBe('未记录');
  });
});
