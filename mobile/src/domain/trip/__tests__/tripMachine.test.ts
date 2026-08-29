import { canTransition, nextStatus, assertTransition } from '../tripMachine';

describe('tripMachine', () => {
  it('PENDING accepts', () => {
    expect(canTransition('PENDING', 'ACCEPT')).toBe(true);
    expect(nextStatus('PENDING', 'ACCEPT')).toBe('IN_TRANSIT');
  });
  it('PENDING cancel', () => {
    expect(nextStatus('PENDING', 'CANCEL')).toBe('CANCELLED');
  });
  it('IN_TRANSIT deliver', () => {
    expect(canTransition('IN_TRANSIT', 'DELIVER')).toBe(true);
    expect(nextStatus('IN_TRANSIT', 'DELIVER')).toBe('COMPLETED');
  });
  it('COMPLETED terminal blocks', () => {
    expect(canTransition('COMPLETED', 'ACCEPT')).toBe(false);
    expect(() => assertTransition('COMPLETED', 'ACCEPT')).toThrow();
  });
});
