import {
  uploadDriverDocument,
  uploadVehicleDocument,
  listDriverDocuments,
  listVehicleDocuments,
} from '../src/services/documentVault';
import { useAuthStore } from '../src/stores/authStore';

const globalFetch = global.fetch;
const globalFormData = global.FormData;

// Local FormData capture — jest/setup.ts intentionally untouched
class CapturingFormData {
  appends: Array<{ key: string; value: any }> = [];
  append(key: string, value: any): void {
    this.appends.push({ key, value });
  }
}

describe('document vault', () => {
  let formInstances: CapturingFormData[];

  beforeEach(async () => {
    formInstances = [];
    (global as any).FormData = class extends CapturingFormData {
      constructor() {
        super();
        formInstances.push(this);
      }
    };
    await useAuthStore.getState().setAuth('tok', {
      id: 'u_1',
      name: 'Raj',
      role: 'driver',
      email: 'r@x.com',
    });
  });

  afterEach(() => {
    global.fetch = globalFetch;
    global.FormData = globalFormData;
  });

  test('rejects invalid doc type before any network call', async () => {
    const fetchMock = jest.fn();
    global.fetch = fetchMock as any;

    await expect(
      uploadDriverDocument('drv_1', { docType: 'passport' as any, fileUri: 'file:///a.jpg' })
    ).rejects.toThrow('INVALID_DOC_TYPE');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('rejects malformed expiry date before any network call', async () => {
    const fetchMock = jest.fn();
    global.fetch = fetchMock as any;

    await expect(
      uploadDriverDocument('drv_1', { docType: 'dl', fileUri: 'file:///dl.jpg', expiryDate: '2026/08/23' })
    ).rejects.toThrow('INVALID_EXPIRY_DATE');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('uploadDriverDocument builds FormData and hits driver endpoint', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    global.fetch = fetchMock as any;

    await uploadDriverDocument('drv_7', {
      docType: 'aadhaar',
      fileUri: 'file:///aadhaar.jpg',
      fileName: 'aadhaar.jpg',
      expiryDate: '2030-05-10',
    });

    const form = formInstances[0];
    const byKey = Object.fromEntries(form.appends.map((a) => [a.key, a.value]));
    expect(byKey.doc_type).toBe('aadhaar');
    expect(byKey.expiry_date).toBe('2030-05-10');
    expect(byKey.file).toMatchObject({
      uri: 'file:///aadhaar.jpg',
      name: 'aadhaar.jpg',
      type: 'image/jpeg',
    });

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/driver/drv_7');
    expect(fetchMock.mock.calls[0][1].method).toBe('POST');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
    expect(fetchMock.mock.calls[0][1].body).toBe(form);
  });

  test('uploadVehicleDocument omits expiry_date when not provided', async () => {
    const fetchMock = jest.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    global.fetch = fetchMock as any;

    await uploadVehicleDocument('veh_3', { docType: 'other', fileUri: 'file:///x.jpg' });

    const form = formInstances[0];
    expect(form.appends.map((a) => a.key)).toEqual(['doc_type', 'file']);
    // Default file name per spec
    expect(form.appends.find((a) => a.key === 'file')!.value.name).toBe('document.jpg');
    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/vehicle/veh_3');
  });

  test('listDriverDocuments parses documents array with auth header', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        documents: [
          { id: 'd1', doc_type: 'pan', file_name: 'pan.jpg', expiry_date: null },
          { id: 'd2', doc_type: 'weird', file_name: 'x.jpg' }, // dropped defensively
        ],
      }),
    });
    global.fetch = fetchMock as any;

    const docs = await listDriverDocuments('drv_7');

    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/driver/drv_7');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok');
    expect(docs).toHaveLength(1);
    expect(docs[0]).toEqual({ id: 'd1', docType: 'pan', fileName: 'pan.jpg', expiryDate: null });
  });

  test('listVehicleDocuments hits vehicle endpoint and returns array', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ documents: [{ id: 'v1', doc_type: 'insurance_copy', expiry_date: '2030-01-01' }] }),
    });
    global.fetch = fetchMock as any;

    const docs = await listVehicleDocuments('veh_3');
    expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/documents/vehicle/veh_3');
    expect(docs).toHaveLength(0); // unknown doc_type filtered
  });
});
