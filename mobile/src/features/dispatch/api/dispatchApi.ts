import { getApiBaseURL } from '../../../constants/network';
import { DispatchOffer } from '../types/dispatch';

export class DispatchApiClient {
  private getHeaders(token: string) {
    return {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    };
  }

  async getPendingOffers(token: string): Promise<DispatchOffer[]> {
    const res = await fetch(`${getApiBaseURL()}/api/v1/drivers/me/offers`, {
      headers: this.getHeaders(token),
    });
    if (!res.ok) {
      throw new Error(`Failed fetching offers: ${res.statusText}`);
    }
    return res.json();
  }
}

export const dispatchApi = new DispatchApiClient();
