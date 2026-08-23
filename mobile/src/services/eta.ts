// ETA prediction — pure deterministic traffic multipliers from product spec.
// Monsoon (June–Sept) ×1.3; rush hours 08:00–10:59 & 17:00–19:59 ×1.25;
// off-peak night 22:00–04:59 ×0.9; otherwise ×1.0. Multipliers compound.

export function trafficMultiplier(d: Date): number {
  let multiplier = 1.0;

  const month = d.getMonth(); // 0-indexed: June=5 .. September=8
  if (month >= 5 && month <= 8) {
    multiplier *= 1.3;
  }

  const hour = d.getHours();
  const isRushHour = (hour >= 8 && hour <= 10) || (hour >= 17 && hour <= 19);
  const isOffPeakNight = hour >= 22 || hour <= 4;

  if (isRushHour) {
    multiplier *= 1.25;
  } else if (isOffPeakNight) {
    multiplier *= 0.9;
  }

  return multiplier;
}

/**
 * Predict trip ETA in whole minutes from departure time.
 * arrivalHintMinutes, when provided, acts as a conservative floor so a
 * known estimate is never undercut by the pure prediction.
 */
export function predictEta(baseMinutes: number, departureTime: Date, arrivalHintMinutes?: number): number {
  if (!Number.isFinite(baseMinutes) || baseMinutes < 0) {
    throw new Error('NEGATIVE_BASE_MINUTES');
  }

  let etaMinutes = baseMinutes * trafficMultiplier(departureTime);
  if (arrivalHintMinutes != null) {
    etaMinutes = Math.max(etaMinutes, arrivalHintMinutes);
  }
  return Math.ceil(etaMinutes);
}
