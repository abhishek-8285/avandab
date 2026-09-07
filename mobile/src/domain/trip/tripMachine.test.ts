import {
  BACKEND_TO_MOBILE,
  MOBILE_TO_BACKEND,
  toMobileStatus,
  canTransition,
} from './tripMachine';

describe('tripMachine backend→mobile collapsed mapping (gap 5)', () => {
  it('covers all 9 backend states', () => {
    expect(Object.keys(BACKEND_TO_MOBILE).sort()).toEqual(
      [
        'assigned',
        'cancelled',
        'completed',
        'delivered',
        'draft',
        'in_transit',
        'reached_pickup',
        'scheduled',
        'started',
      ].sort(),
    );
  });

  it('collapses to the documented 4 mobile states', () => {
    expect(BACKEND_TO_MOBILE).toEqual({
      draft: 'PENDING',
      scheduled: 'PENDING',
      assigned: 'PENDING',
      started: 'IN_TRANSIT',
      reached_pickup: 'IN_TRANSIT',
      in_transit: 'IN_TRANSIT',
      delivered: 'COMPLETED',
      completed: 'COMPLETED',
      cancelled: 'CANCELLED',
    });
  });

  it('toMobileStatus normalises known + unknown raws', () => {
    expect(toMobileStatus('draft')).toBe('PENDING');
    expect(toMobileStatus('scheduled')).toBe('PENDING');
    expect(toMobileStatus('in_transit')).toBe('IN_TRANSIT');
    expect(toMobileStatus('completed')).toBe('COMPLETED');
    expect(toMobileStatus('cancelled')).toBe('CANCELLED');
    expect(toMobileStatus('bogus')).toBe('PENDING');
  });

  it('outbound hint maps each mobile state to a canonical backend status', () => {
    expect(MOBILE_TO_BACKEND).toEqual({
      PENDING: 'assigned',
      IN_TRANSIT: 'in_transit',
      COMPLETED: 'completed',
      CANCELLED: 'cancelled',
    });
  });

  it('does not change state-machine guards', () => {
    expect(canTransition('PENDING', 'START')).toBe(true);
    expect(canTransition('COMPLETED', 'START')).toBe(false);
  });
});
