export interface User {
  id: string;
  username: string;
  token?: string;
  scope?: string; // 'FULL', 'RECOVERY'
  dashboardScenarioId?: string;
  dashboardMonthOffset?: number;
}

console.log("AUTH STORE: Initializing Rune-based Store...");

class AuthStore {
  user = $state<User | null>(null);
  isAuthenticated = $state(false);
  isLoading = $state(true);

  constructor() {
    console.log("AUTH STORE: Instance created");
  }

  login(userData: User) {
    this.user = userData;
    this.isAuthenticated = true;
    this.isLoading = false;

    if (typeof window !== "undefined" && userData.token) {
      localStorage.setItem("auth_token", userData.token);
    }
  }

  logout() {
    this.user = null;
    this.isAuthenticated = false;
    this.isLoading = false;

    if (typeof window !== "undefined") {
      localStorage.removeItem("auth_token");
    }
  }

  setLoading(loading: boolean) {
    this.isLoading = loading;
  }
}

export const auth = new AuthStore();
