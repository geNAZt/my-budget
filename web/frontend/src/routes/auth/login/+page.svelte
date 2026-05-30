<script lang="ts">
	import { login } from '$lib/utils/auth.svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { goto } from '$app/navigation';
	import { Fingerprint, Loader2, AlertCircle, KeyRound, ArrowRight, ShieldCheck } from '@lucide/svelte';
	import { fade, slide } from 'svelte/transition';

	let username = $state('');
	let isLoading = $state(false);
	let error = $state<string | null>(null);

	async function handleLogin(e: SubmitEvent) {
		e.preventDefault();
		if (!username) return;

		isLoading = true;
		error = null;

		try {
			await login(username);
			// After successful login, the deriveMasterKey function in auth.ts
			// will have already updated the auth store with the master key.
			goto('/dashboard');
		} catch (err: any) {
			error = err.message || 'Authentication failed. Please check your username or try another device.';
		} finally {
			isLoading = false;
		}
	}
</script>

<div class="min-h-[calc(100vh-64px)] flex items-center justify-center px-4 py-12 relative overflow-hidden">
    <!-- Ambient Background -->
    <div class="absolute inset-0 z-0 pointer-events-none">
        <div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-full h-full bg-gradient-radial from-indigo-50/50 to-transparent"></div>
    </div>

	<div class="w-full max-w-lg relative z-10">
        <div class="glass-card p-10 md:p-12 relative" in:fade={{ duration: 400 }}>
            <!-- Top branding accent -->
            <div class="absolute top-0 left-0 right-0 h-1.5 bg-gradient-to-r from-indigo-600 to-purple-600"></div>

            <div class="text-center mb-12">
                <div class="inline-flex items-center justify-center p-4 bg-indigo-50 rounded-3xl mb-6 shadow-sm">
                    <KeyRound class="h-12 w-12 text-indigo-600" />
                </div>
                <h1 class="text-4xl font-black text-slate-900 tracking-tight">Identity Vault</h1>
                <p class="text-slate-500 font-medium mt-2">Enter your username to begin verification</p>
            </div>

            <form onsubmit={handleLogin} class="space-y-6">
                <div class="space-y-2">
                    <label for="username" class="text-sm font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Username</label>
                    <div class="relative group">
                        <div class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
                            <ShieldCheck class="h-5 w-5 text-slate-400 group-focus-within:text-indigo-500 transition-colors" />
                        </div>
                        <input
                            type="text"
                            id="username"
                            bind:value={username}
                            placeholder="Fabian"
                            class="block w-full pl-12 pr-4 py-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-medium"
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
                        disabled={isLoading || !username}
                        class="btn-primary w-full py-5 text-xl shadow-2xl shadow-indigo-100 group"
                    >
                        {#if isLoading}
                            <Loader2 class="h-6 w-6 animate-spin" />
                            <span>Authenticating...</span>
                        {:else}
                            <span>Verify Identity</span>
                            <ArrowRight class="w-5 h-5 group-hover:translate-x-1 transition-transform" />
                        {/if}
                    </button>

                    <div class="text-center space-y-4">
                        <p class="text-slate-500 font-medium">
                            First time here?
                            <a href="/auth/register" class="text-indigo-600 font-black hover:text-indigo-500 transition-colors border-b-2 border-indigo-100 hover:border-indigo-500 ml-1">
                                Create Account
                            </a>
                        </p>
                        <p>
                            <a href="/auth/recovery" class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 hover:text-indigo-600 transition-all">
                                Lost your passkey? Use Recovery Token
                            </a>
                        </p>
                    </div>
                </div>
            </form>
        </div>

        <!-- Footer badging -->
        <div class="mt-10 flex justify-center items-center gap-6 opacity-40">
            <div class="flex items-center gap-2">
                <Fingerprint class="h-4 w-4 text-slate-600" />
                <span class="text-[10px] font-black uppercase tracking-[0.2em]">Zero Password</span>
            </div>
        </div>
	</div>
</div>
