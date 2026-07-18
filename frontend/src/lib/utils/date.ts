export function toInputMonth(isoStr: string | null | undefined): string {
    if (!isoStr) return "";
    return isoStr.substring(0, 7); // "YYYY-MM"
}

export function fromInputMonth(val: string | null | undefined): string {
    if (!val) return "";
    return val + "-01T00:00:00Z";
}
