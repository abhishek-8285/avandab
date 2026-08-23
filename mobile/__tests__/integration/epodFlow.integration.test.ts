// Integration layer: ePOD pipeline — queue persistence → multipart flush → API contract.
// Verifies multiple units work together WITHOUT rendering UI:
// OfflineQueue (SQLite mock) → FormData assembly → fetch transport → queue clearing.
import { OfflineQueue } from '../../src/services/offlineQueue';
import { getApiBaseURL } from '../../src/constants/network';
import { resetSQLiteMockState } from '../../jest/setup';
import { useAuthStore } from '../../src/stores/authStore';

const globalFetch = global.fetch;
const RealFormData = global.FormData;

const POD_PAYLOAD = {
  consignee_name: 'Ramesh',
  consignee_phone: '+919812345678',
  notes: 'Delivered at gate 2, receiver present',
  photo_uri: 'file:///tmp/pod.jpg',
  pod_signature_data: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==',
  quantity_short: 2,
  damage_qty: 1,
  refusal_reason: null,
  latitude: 18.5204,
  longitude: 73.8567,
} as const;

/** Wraps RN FormData so every append is recorded without changing behavior. */
class RecordingFormData extends RealFormData {
  static records: { name: string; value: unknown }[] = [];

  append(name: string, value: any): void {
    RecordingFormData.records.push({ name, value });
    super.append(name, value);
  }

  static field(name: string): { value: unknown } | undefined {
    return RecordingFormData.records.find((r) => r.name === name);
  }
}

describe('ePOD flow integration (queue → flush → deliver-pod)', () => {
  beforeEach(async () => {
    resetSQLiteMockState();
    RecordingFormData.records = [];
    await OfflineQueue.init();
    await useAuthStore.getState().setAuth('mock_token_123', {
      id: 'u_1',
      name: 'Rajesh Kumar',
      role: 'driver',
      email: 'driver@avandab.com',
      driverId: 'drv_1',
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
    global.FormData = RealFormData;
  });

  test('enqueue → pending row keeps every spec-mandated field intact', async () => {
    await OfflineQueue.enqueuePOD('trip-777', POD_PAYLOAD);

    const pods = await OfflineQueue.pendingPODs();
    expect(pods).toHaveLength(1);
    const pod = pods[0];
    expect(pod.trip_id).toBe('trip-777');
    expect(pod.consignee_name).toBe('Ramesh');
    expect(pod.consignee_phone).toBe('+919812345678');
    expect(pod.notes).toBe('Delivered at gate 2, receiver present');
    expect(pod.photo_uri).toBe('file:///tmp/pod.jpg');
    expect(pod.pod_signature_data).toBe(POD_PAYLOAD.pod_signature_data);
    expect(pod.quantity_short).toBe(2);
    expect(pod.damage_qty).toBe(1);
    expect(pod.refusal_reason).toBeNull();
    expect(pod.latitude).toBe(18.5204);
    expect(pod.longitude).toBe(73.8567);
    expect(typeof pod.id).toBe('number');
    expect(typeof pod.created_at).toBe('string');
  });

  test('flush POSTs one multipart request with all fields, then clears; second flush is a no-op', async () => {
    await OfflineQueue.enqueuePOD('trip-777', POD_PAYLOAD);

    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: 'delivered' }),
    });
    global.fetch = fetchMock as any;
    global.FormData = RecordingFormData as any;

    const result = await OfflineQueue.flush();

    expect(result.podsFlushed).toBe(1);

    // Exactly ONE network call to the exact ePOD endpoint.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${getApiBaseURL()}/api/v1/trips/trip-777/deliver-pod`);
    expect(init.method).toBe('POST');
    expect(init.headers.Authorization).toBe('Bearer mock_token_123');

    // Every spec-mandated form key travels on the wire.
    const expectedStringFields: [string, string][] = [
      ['consignee_name', 'Ramesh'],
      ['consignee_phone', '+919812345678'],
      ['notes', 'Delivered at gate 2, receiver present'],
      ['pod_signature_data', POD_PAYLOAD.pod_signature_data],
      ['signature_dataurl', POD_PAYLOAD.pod_signature_data],
      ['latitude', '18.5204'],
      ['longitude', '73.8567'],
      ['quantity_short', '2'],
      ['damage_qty', '1'],
    ];
    for (const [name, value] of expectedStringFields) {
      expect(RecordingFormData.field(name)).toEqual({ name, value });
    }
    // Photo rides along as an RN-style attachment object.
    expect(RecordingFormData.field('pod_photo')?.value).toMatchObject({
      uri: 'file:///tmp/pod.jpg',
      name: 'pod.jpg',
      type: 'image/jpeg',
    });
    // refusal_reason was null — it must NOT be appended at all.
    expect(RecordingFormData.field('refusal_reason')).toBeUndefined();

    // Queue drained after successful flush.
    expect(await OfflineQueue.pendingPODs()).toEqual([]);

    // Second flush makes ZERO fetch calls (idempotent drain).
    await OfflineQueue.flush();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test('server rejection keeps the POD queued (no data loss)', async () => {
    await OfflineQueue.enqueuePOD('trip-777', POD_PAYLOAD);

    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 422,
      json: async () => ({ error: 'invalid' }),
    }) as any;

    const result = await OfflineQueue.flush();
    expect(result.podsFlushed).toBe(0);
    const remaining = await OfflineQueue.pendingPODs();
    expect(remaining).toHaveLength(1);
    expect(remaining[0].consignee_name).toBe('Ramesh');
  });
});
