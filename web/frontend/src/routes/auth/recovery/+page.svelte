<script lang="ts">
	import { recoveryLogin } from '$lib/utils/auth.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { goto } from '$app/navigation';
	import { LifeBuoy, Loader2, AlertCircle, KeyRound, ArrowRight, ShieldCheck, User } from '@lucide/svelte';
	import { fade, slide } from 'svelte/transition';

	let username = $state('');
	let token = $state('');
	let isLoading = $state(false);
	let error = $state<string | null>(null);

	async function handleRecovery(e: SubmitEvent) {
		e.preventDefault();
		if (!username || !token) return;

		isLoading = true;
		error = null;

		try {
			await recoveryLogin(username, token.toUpperCase().trim());
			// After recovery login, we are in a restricted session.
			// The layout will detect this and show the "Force Update Key" UI.
			goto('/dashboard');
		} catch (err: any) {
			error = err.message || 'Recovery failed. Please check your username and token.';
		} finally {
			isLoading = false;
		}
	}
</script>

<div class="min-h-[calc(100vh-64px)] flex items-center justify-center px-4 py-12 relative overflow-hidden">
    <!-- Ambient Background -->
    <div class="absolute inset-0 z-0 pointer-events-none">
        <div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-full h-full bg-gradient-radial from-amber-50/50 to-transparent"></div>
    </div>

	<div class="w-full max-w-lg relative z-10">
        <div class="glass-card p-10 md:p-12 relative" in:fade={{ duration: 400 }}>
            <div class="absolute top-0 left-0 right-0 h-1.5 bg-gradient-to-r from-amber-500 to-orange-600"></div>

            <div class="text-center mb-12">
                <div class="inline-flex items-center justify-center p-4 bg-amber-50 rounded-3xl mb-6 shadow-sm">
                    <LifeBuoy class="h-12 w-12 text-amber-600" />
                </div>
                <h1 class="text-4xl font-black text-slate-900 tracking-tight">Account Recovery</h1>
                <p class="text-slate-500 font-medium mt-2">Enter your one-time recovery token</p>
            </div>

            <form onsubmit={handleRecovery} class="space-y-6">
                <div class="space-y-2">
                    <label for="username" class="text-sm font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Username</label>
                    <div class="relative group">
                        <div class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
                            <User class="h-5 w-5 text-slate-400 group-focus-within:text-amber-500 transition-colors" />
                        </div>
                        <input
                            type="text"
                            id="username"
                            bind:value={username}
                            placeholder="Account name"
                            class="block w-full pl-12 pr-4 py-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-medium"
                            required
                        />
                    </div>
                </div>

                <div class="space-y-2">
                    <label for="token" class="text-sm font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Recovery Token</label>
                    <div class="relative group">
                        <div class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
                            <KeyRound class="h-5 w-5 text-slate-400 group-focus-within:text-amber-500 transition-colors" />
                        </div>
                        <input
                            type="text"
                            id="token"
                            bind:value={token}
                            placeholder="MB-XXXX-XXXX-XXXX"
                            class="block w-full pl-12 pr-4 py-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-mono font-bold tracking-[0.2em] uppercase"
                            required
                        />
                    </div>
                </div>

                {#if error}
                    <div class="bg-red-50 text-red-700 p-4 rounded-2xl flex items-start gap-3 text-sm border border-red-100" transition:slide>
                        <AlertCircle class="h-5 w-5 flex-shrink-0" />
                        <span class="font-bold">{error}</span>
                    </div>
                {/if}

                <div class="space-y-6 pt-4">
                    <button
                        type="submit"
                        disabled={isLoading || !username || !token}
                        class="btn-primary w-full py-5 text-xl shadow-2xl shadow-amber-100 group !bg-amber-600 !border-amber-600 hover:!bg-amber-700"
                    >
                        {#if isLoading}
                            <Loader2 class="h-6 w-6 animate-spin" />
                            <span>Verifying Token...</span>
                        {:else}
                            <span>Verify and Login</span>
                            <ArrowRight class="w-5 h-5 group-hover:translate-x-1 transition-transform" />
                        {/if}
                    </button>

                    <div class="text-center">
                        <a href="/auth/login" class="text-slate-400 font-bold hover:text-slate-600 transition-colors text-sm">
                            Back to login
                        </a>
                    </div>
                </div>
            </form>
        </div>

        <div class="mt-10 flex justify-center items-center gap-6 opacity-40">
            <div class="flex items-center gap-2">
                <ShieldCheck class="w-4 h-4 text-slate-600" />
                <span class="text-[10px] font-black uppercase tracking-[0.2em]">One-Time Recovery</span>
            </div>
        </div>
	</div>
</div>
