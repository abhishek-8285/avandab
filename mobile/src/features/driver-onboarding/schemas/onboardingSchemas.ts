export const validateLicenseNumber = (dl: string): boolean => {
  const clean = dl.trim().toUpperCase();
  return clean.length >= 6 && clean.length <= 20;
};

export const validateVehiclePlate = (plate: string): boolean => {
  const clean = plate.trim().toUpperCase().replace(/[^A-Z0-9]/g, '');
  return clean.length >= 6 && clean.length <= 12;
};

export const validateIFSC = (ifsc: string): boolean => {
  const clean = ifsc.trim().toUpperCase();
  return /^[A-Z]{4}0[A-Z0-9]{6}$/.test(clean) || clean.length >= 8;
};

export const validateAccountNumber = (acc: string): boolean => {
  const clean = acc.trim();
  return /^\d{6,20}$/.test(clean);
};
