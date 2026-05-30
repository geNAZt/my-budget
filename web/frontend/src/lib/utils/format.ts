export function formatGermanAmount(val: number): string {
  return val.toLocaleString("de-DE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function parseGermanAmount(str: string | number): number {
  if (typeof str === "number") return str;
  if (!str) return 0;
  const normalized = str.toString().replace(/\./g, "").replace(",", ".");
  return parseFloat(normalized) || 0;
}
