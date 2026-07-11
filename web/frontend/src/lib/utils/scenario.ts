import { auth } from "$lib/stores/auth.svelte";

export function resolveActiveScenario(scenarios: any[]): any {
  if (!scenarios || scenarios.length === 0) return null;
  const preferredId = auth.user?.dashboardScenarioId || (typeof localStorage !== "undefined" ? localStorage.getItem("dashboard_scenario_id") : null);
  if (preferredId) {
    const found = scenarios.find((s) => s.id === preferredId);
    if (found) return found;
  }
  return scenarios.find((s) => s.isActive) || scenarios[0];
}

export function savePreferredScenario(id: string) {
  if (typeof localStorage !== "undefined") {
    localStorage.setItem("dashboard_scenario_id", id);
  }
}
