declare module 'wealthengine' {
	export class Income {
		plannedValue(): number;
	}

	export class BudgetSheet {
		income(name: string): Income;
	}

	export class RealtimeAccount {
		balance(): number;
		sync(): Promise<void>;
	}

	export class Realtime {
		account(key: string): RealtimeAccount;
	}

	export class WealthEngine {
		currentBudgetSheet(): BudgetSheet;
		realtime(): Realtime;
	}
}

// Injected globals
declare const secrets: Record<string, string>;
declare const trigger: {
	type: 'CRON' | 'TRANSACTION' | 'SYNC_FINISHED';
	data: {
		id?: string;
		amount?: number;
		receiver?: string;
		description?: string;
		integration_id: string;
		account_id?: string;
		timestamp: string;
		integration_name?: string;
		service_type?: string;
		discovered_transactions?: number;
	};
};
