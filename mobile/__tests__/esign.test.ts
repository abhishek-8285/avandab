import { initateESign, pollESignStatus, maskAadhaar } from '../src/services/esign';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;

describe('esign service', () => {
  afterEach(() => {
    global.fetch = globalFetch;
  });

  test('initateESign posts empty JSON body to document esign endpoint with bearer', async () => {
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
    });
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ request_id: 'req_1' }),
    });
    global.fetch = fetchMock as any;

    await initateESign('doc_9');

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/doc_9/esign');
    expect(fetchMock.mock.calls[0][1].method).toBe('POST');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
    expect(fetchMock.mock.calls[0][1].body).toBe(JSON.stringify({}));
  });

  test('initateESign maps snake_case response incl audit trail', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        request_id: 'req_42',
        document_id: 'doc_9',
        status: 'otp_sent',
        esign_url: 'https://uidai.example/esign',
        masked_aadhaar: 'XXXX XXXX 1234',
        audit_trail: {
          timestamp: '2026-08-23T10:00:00Z',
          ip_address: '10.0.0.7',
          certificate_details: 'UIDAI Licensed CA',
        },
      }),
    });
    global.fetch = fetchMock as any;

    const req = await initateESign('doc_9');

    expect(req.requestId).toBe('req_42');
    expect(req.documentId).toBe('doc_9');
    expect(req.status).toBe('otp_sent');
    expect(req.esignUrl).toBe('https://uidai.example/esign');
    expect(req.maskedAadhaar).toBe('XXXX XXXX 1234');
    expect(req.auditTrail).toEqual({
      timestamp: '2026-08-23T10:00:00Z',
      ipAddress: '10.0.0.7',
      certificateDetails: 'UIDAI Licensed CA',
    });
  });

  test('unknown status falls back to pending', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ request_id: 'r1', document_id: 'd1', status: 'weird' }),
    });
    global.fetch = fetchMock as any;

    const req = await initateESign('doc_1');
    expect(req.status).toBe('pending');
  });

  test('non-ok response throws ESIGN_INIT_FAILED', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: false, status: 500 });
    global.fetch = fetchMock as any;

    await expect(initateESign('doc_9')).rejects.toThrow('ESIGN_INIT_FAILED');
  });

  test('pollESignStatus hits status endpoint with bearer', async () => {
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
    });
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        request_id: 'req_42',
        document_id: 'doc_9',
        status: 'signed',
        audit_trail: { timestamp: '2026-08-23T10:05:00Z' },
      }),
    });
    global.fetch = fetchMock as any;

    const req = await pollESignStatus('req_42');

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/esign/req_42/status');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
    expect(req.status).toBe('signed');
    expect(req.auditTrail).toEqual({ timestamp: '2026-08-23T10:05:00Z', ipAddress: undefined, certificateDetails: undefined });
  });

  test('pollESignStatus 404 throws ESIGN_REQUEST_NOT_FOUND', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: false, status: 404 });
    global.fetch = fetchMock as any;

    await expect(pollESignStatus('missing')).rejects.toThrow('ESIGN_REQUEST_NOT_FOUND');
  });

  test('pollESignStatus other non-ok throws HTTP message', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: false, status: 503 });
    global.fetch = fetchMock as any;

    await expect(pollESignStatus('req_1')).rejects.toThrow('Server returned HTTP 503');
  });
});

describe('maskAadhaar', () => {
  test('12 digits → XXXX XXXX last4', () => {
    expect(maskAadhaar('123456781234')).toBe('XXXX XXXX 1234');
    expect(maskAadhaar('999900001111')).toBe('XXXX XXXX 1111');
  });

  test('short input → null', () => {
    expect(maskAadhaar('1234')).toBeNull();
  });

  test('long input → null', () => {
    expect(maskAadhaar('12345678123456')).toBeNull();
  });

  test('garbage → null', () => {
    expect(maskAadhaar('abcd56781234')).toBeNull();
    expect(maskAadhaar('')).toBeNull();
    expect(maskAadhaar('1234 5678 123a')).toBeNull();
  });

  test('already-masked passthrough trimmed', () => {
    expect(maskAadhaar('XXXX XXXX 1234')).toBe('XXXX XXXX 1234');
    expect(maskAadhaar('  XXXX XXXX 9999  ')).toBe('XXXX XXXX 9999');
  });
});
