import { extractApiErrorMessage } from './api';

describe('extractApiErrorMessage', () => {
  it('returns fallback for empty input', () => {
    expect(extractApiErrorMessage(null, 'fallback')).toBe('fallback');
    expect(extractApiErrorMessage('', 'fallback')).toBe('fallback');
  });

  it('returns plain string bodies', () => {
    expect(extractApiErrorMessage('plain error', 'fallback')).toBe('plain error');
  });

  it('reads top-level message', () => {
    expect(extractApiErrorMessage({ message: 'top level' }, 'fallback')).toBe('top level');
  });

  it('reads flat middleware shape', () => {
    expect(
      extractApiErrorMessage(
        {
          success: false,
          error: { error: 'internal_server_error', message: 'Internal server error occurred', code: 500 },
        },
        'fallback',
      ),
    ).toBe('Internal server error occurred');
  });

  it('reads nested core ErrorResponse under APIResponse (the PSBT regression)', () => {
    // models.NewErrorResponse embeds core.ErrorResponse which itself has an "error" field,
    // producing: { success:false, error: { error: { code, message, ... } } }
    const body = {
      success: false,
      error: {
        error: {
          code: '400',
          message: 'invalid change address: decoded address is of unknown format',
          details: {},
          timestamp: '2026-07-08T00:00:00Z',
          request_id: '',
        },
      },
    };
    expect(extractApiErrorMessage(body, `HTTP 400`)).toBe(
      'invalid change address: decoded address is of unknown format',
    );
  });

  it('does not return [object Object] for nested objects', () => {
    const msg = extractApiErrorMessage(
      { error: { error: { code: '403', message: 'invalid api key' } } },
      'HTTP 403',
    );
    expect(msg).toBe('invalid api key');
    expect(msg).not.toContain('[object Object]');
  });

  it('falls back when nothing useful is present', () => {
    expect(extractApiErrorMessage({ success: false }, 'HTTP 500')).toBe('HTTP 500');
  });
});
