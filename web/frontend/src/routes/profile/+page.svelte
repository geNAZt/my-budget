<script lang="ts">
    import { onMount } from "svelte";
    import { wsCall } from "$lib/utils/ws_fetch";
    import { auth } from "$lib/stores/auth.svelte";
    import { upgradeSecurityKey } from "$lib/utils/auth.svelte";
    import * as api from "$lib/gen/api_pb.js";
    import {
        Settings,
        KeyRound,
        Clock,
        Trash2,
        Fingerprint,
        ShieldCheck,
        Plus,
        RefreshCw,
        Eye,
        EyeOff,
        Check,
        AlertCircle,
        Loader2,
        Pencil,
        Globe,
        User,
        Copy,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    let profile = $state<any>(null);
    let isLoading = $state(true);
    let isUpdatingTimezone = $state(false);
    let isCyclingRecovery = $state(false);
    let showNewToken = $state(false);
    let newToken = $state("");
    let error = $state<string | null>(null);

    let editPasskeyId = $state<string | null>(null);
    let editPasskeyName = $state("");

    const timezones = Intl.supportedValuesOf("timeZone").map((tz) => ({
        id: tz,
        name: tz,
    }));

    onMount(async () => {
        await fetchProfile();
    });

    async function fetchProfile() {
        isLoading = true;
        try {
            const [resp, err] = await wsCall(
                "user::profile::get",
                api.GenericIDSchema,
                { id: "" },
                [api.UserProfileSchema],
            ).one();
            if (err) throw err;
            profile = resp;
        } catch (e: any) {
            error = e.message || "Failed to load profile";
        } finally {
            isLoading = false;
        }
    }

    async function updateTimezone(tz: string) {
        isUpdatingTimezone = true;
        try {
            const [, err] = await wsCall(
                "user::profile::update",
                api.UpdateProfileRequestSchema,
                { timezone: tz },
                [api.GenericIDSchema],
            ).one();
            if (err) throw err;
            profile.timezone = tz;
        } catch (e: any) {
            error = e.message || "Failed to update timezone";
        } finally {
            isUpdatingTimezone = false;
        }
    }

    async function deletePasskey(id: string) {
        if (!confirm("Are you sure you want to remove this passkey?")) return;
        try {
            const [, err] = await wsCall(
                "user::profile::delete_authenticator",
                api.GenericIDSchema,
                { id },
                [api.GenericIDSchema],
            ).one();
            if (err) throw err;
            await fetchProfile();
        } catch (e: any) {
            alert(e.message || "Failed to delete passkey");
        }
    }

    async function renamePasskey() {
        if (!editPasskeyId || !editPasskeyName) return;
        try {
            const [, err] = await wsCall(
                "user::profile::rename_authenticator",
                api.RenameAuthenticatorRequestSchema,
                { id: editPasskeyId, name: editPasskeyName },
                [api.GenericIDSchema],
            ).one();
            if (err) throw err;
            editPasskeyId = null;
            await fetchProfile();
        } catch (e: any) {
            alert(e.message || "Failed to rename passkey");
        }
    }

    async function addPasskey() {
        try {
            await upgradeSecurityKey();
            await fetchProfile();
        } catch (e: any) {
            alert(e.message || "Failed to add passkey");
        }
    }

    async function cycleRecovery() {
        if (
            !confirm(
                "This will invalidate your current recovery key. Are you sure?",
            )
        )
            return;
        isCyclingRecovery = true;
        showNewToken = false;
        try {
            const [resp, err] = await wsCall(
                "user::profile::cycle_recovery",
                api.GenericIDSchema,
                { id: "" },
                [api.CycleRecoveryResponseSchema],
            ).one();
            if (err) throw err;
            newToken = resp.newRecoveryToken;
            showNewToken = true;
        } catch (e: any) {
            error = e.message || "Failed to cycle recovery key";
        } finally {
            isCyclingRecovery = false;
        }
    }
</script>

<div class="min-h-screen bg-slate-50/50 pb-20 pt-8 px-4 md:px-8">
    <div class="max-w-4xl mx-auto space-y-8">
        <!-- Header -->
        <div class="flex items-center justify-between">
            <div>
                <h1 class="text-3xl font-black text-slate-900 tracking-tight flex items-center gap-3">
                    <User class="w-8 h-8 text-indigo-600" />
                    User Profile
                </h1>
                <p class="text-slate-500 font-medium mt-1">Manage your account settings and security</p>
            </div>
            <a
                href="/dashboard"
                class="px-4 py-2 bg-white border border-slate-200 text-slate-600 rounded-xl font-bold text-xs uppercase tracking-wider hover:bg-slate-50 transition-colors shadow-sm"
            >
                Back to Dashboard
            </a>
        </div>

        {#if isLoading}
            <div class="flex flex-col items-center justify-center py-20 bg-white rounded-[2rem] border border-slate-200/50 shadow-sm">
                <Loader2 class="w-12 h-12 text-indigo-600 animate-spin" />
                <p class="text-slate-500 font-black uppercase tracking-widest text-[10px] mt-4">Loading Profile...</p>
            </div>
        {:else if profile}
            <!-- Timezone Section -->
            <div class="bg-white rounded-[2.5rem] border border-slate-200/60 shadow-sm overflow-hidden" in:fade>
                <div class="p-8 md:p-10">
                    <div class="flex items-center gap-4 mb-8">
                        <div class="p-3 bg-indigo-50 rounded-2xl">
                            <Globe class="w-6 h-6 text-indigo-600" />
                        </div>
                        <div>
                            <h2 class="text-xl font-black text-slate-900">Regional Settings</h2>
                            <p class="text-slate-500 text-sm font-medium">Configure your local timezone for projections</p>
                        </div>
                    </div>

                    <div class="grid md:grid-cols-2 gap-8 items-end">
                        <div class="space-y-3">
                            <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Current Timezone</label>
                            <SearchableDropdown
                                items={timezones}
                                selectedId={profile.timezone}
                                placeholder="Search timezone..."
                                onChange={(tz) => updateTimezone(tz)}
                            />
                        </div>
                        <div class="flex items-center gap-3 text-xs text-slate-400 font-medium bg-slate-50 p-4 rounded-2xl border border-slate-100">
                            <Clock class="w-4 h-4 text-indigo-400" />
                            Current local time: {new Date().toLocaleTimeString([], { timeZone: profile.timezone })}
                        </div>
                    </div>
                </div>
            </div>

            <!-- Passkeys Section -->
            <div class="bg-white rounded-[2.5rem] border border-slate-200/60 shadow-sm overflow-hidden" in:fade={{ delay: 100 }}>
                <div class="p-8 md:p-10">
                    <div class="flex items-center justify-between mb-8">
                        <div class="flex items-center gap-4">
                            <div class="p-3 bg-emerald-50 rounded-2xl">
                                <Fingerprint class="w-6 h-6 text-emerald-600" />
                            </div>
                            <div>
                                <h2 class="text-xl font-black text-slate-900">Security Keys</h2>
                                <p class="text-slate-500 text-sm font-medium">Manage your hardware-bound passkeys</p>
                            </div>
                        </div>
                        <button
                            onclick={addPasskey}
                            class="flex items-center gap-2 px-5 py-3 bg-emerald-600 text-white rounded-2xl font-black text-[10px] uppercase tracking-wider hover:bg-emerald-700 transition-all shadow-lg shadow-emerald-100 group"
                        >
                            <Plus class="w-4 h-4" />
                            Add New Key
                        </button>
                    </div>

                    <div class="space-y-4">
                        {#each profile.authenticators as auth}
                            <div class="group flex items-center justify-between p-6 bg-slate-50/50 border border-slate-100 rounded-[2rem] hover:bg-white hover:border-indigo-200 hover:shadow-md transition-all">
                                <div class="flex items-center gap-5">
                                    <div class="p-4 bg-white rounded-2xl shadow-sm border border-slate-100 group-hover:border-indigo-100 group-hover:bg-indigo-50/30 transition-colors">
                                        <ShieldCheck class="w-6 h-6 text-indigo-500" />
                                    </div>
                                    <div>
                                        {#if editPasskeyId === auth.id}
                                            <div class="flex items-center gap-2">
                                                <input
                                                    type="text"
                                                    bind:value={editPasskeyName}
                                                    class="px-3 py-1.5 bg-white border border-indigo-300 rounded-lg text-sm font-bold focus:ring-2 focus:ring-indigo-500/20 outline-none"
                                                    autofocus
                                                />
                                                <button
                                                    onclick={renamePasskey}
                                                    class="p-1.5 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700"
                                                >
                                                    <Check class="w-4 h-4" />
                                                </button>
                                                <button
                                                    onclick={() => editPasskeyId = null}
                                                    class="p-1.5 bg-slate-200 text-slate-600 rounded-lg hover:bg-slate-300"
                                                >
                                                    <Trash2 class="w-4 h-4" />
                                                </button>
                                            </div>
                                        {:else}
                                            <div class="flex items-center gap-2">
                                                <h3 class="font-black text-slate-900">{auth.name}</h3>
                                                <button
                                                    onclick={() => {
                                                        editPasskeyId = auth.id;
                                                        editPasskeyName = auth.name;
                                                    }}
                                                    class="opacity-0 group-hover:opacity-100 p-1 text-slate-400 hover:text-indigo-600 transition-all"
                                                >
                                                    <Pencil class="w-3.5 h-3.5" />
                                                </button>
                                            </div>
                                        {/if}
                                        <p class="text-[10px] text-slate-400 font-bold uppercase tracking-wider mt-0.5">Added: {auth.createdAt}</p>
                                    </div>
                                </div>
                                <button
                                    onclick={() => deletePasskey(auth.id)}
                                    class="p-3 text-slate-300 hover:text-rose-600 hover:bg-rose-50 rounded-xl transition-all opacity-0 group-hover:opacity-100"
                                    title="Remove key"
                                >
                                    <Trash2 class="w-5 h-5" />
                                </button>
                            </div>
                        {/each}
                    </div>
                </div>
            </div>

            <!-- Recovery Section -->
            <div class="bg-white rounded-[2.5rem] border border-slate-200/60 shadow-sm overflow-hidden" in:fade={{ delay: 200 }}>
                <div class="p-8 md:p-10">
                    <div class="flex items-center gap-4 mb-8">
                        <div class="p-3 bg-rose-50 rounded-2xl">
                            <KeyRound class="w-6 h-6 text-rose-600" />
                        </div>
                        <div>
                            <h2 class="text-xl font-black text-slate-900">Recovery & Backup</h2>
                            <p class="text-slate-500 text-sm font-medium">Emergency access tokens for device loss</p>
                        </div>
                    </div>

                    <div class="p-6 bg-slate-50 rounded-[2rem] border border-slate-100 space-y-6">
                        <div class="flex items-start gap-4">
                            <div class="p-2 bg-amber-100 rounded-lg">
                                <AlertCircle class="w-5 h-5 text-amber-600" />
                            </div>
                            <div class="flex-1">
                                <h4 class="text-xs font-black text-slate-900 uppercase tracking-wider">Warning</h4>
                                <p class="text-xs text-slate-500 leading-relaxed font-medium mt-1">
                                    Recovery tokens allow access to your account without a passkey. Store them securely offline (e.g., in a password manager or on paper). Anyone with this token can access your data.
                                </p>
                            </div>
                        </div>

                        {#if showNewToken}
                            <div class="bg-indigo-900 p-8 rounded-[1.5rem] text-center space-y-4" in:slide>
                                <p class="text-indigo-200 text-[10px] font-black uppercase tracking-[0.3em]">New Recovery Token</p>
                                <div class="flex items-center justify-center gap-3">
                                    <code class="text-2xl md:text-3xl font-black text-white tracking-[0.2em] font-mono">{newToken}</code>
                                    <button
                                        onclick={() => navigator.clipboard.writeText(newToken)}
                                        class="p-2 text-indigo-300 hover:text-white hover:bg-white/10 rounded-lg transition-colors"
                                        title="Copy to clipboard"
                                    >
                                        <Copy class="w-6 h-6" />
                                    </button>
                                </div>
                                <p class="text-indigo-300 text-xs font-medium">This token will only be shown once. Please save it now.</p>
                                <button
                                    onclick={() => showNewToken = false}
                                    class="px-6 py-2 bg-white/10 hover:bg-white/20 text-white text-[10px] font-black uppercase tracking-wider rounded-xl transition-colors"
                                >
                                    I have saved the token
                                </button>
                            </div>
                        {:else}
                            <div class="flex items-center justify-between">
                                <div class="flex items-center gap-3">
                                    <div class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></div>
                                    <span class="text-xs font-black text-slate-700 uppercase tracking-widest">Recovery Active</span>
                                </div>
                                <button
                                    onclick={cycleRecovery}
                                    disabled={isCyclingRecovery}
                                    class="flex items-center gap-2 px-6 py-3 bg-white border border-slate-200 text-slate-900 rounded-2xl font-black text-[10px] uppercase tracking-wider hover:border-rose-300 hover:text-rose-600 transition-all shadow-sm"
                                >
                                    {#if isCyclingRecovery}
                                        <RefreshCw class="w-4 h-4 animate-spin" />
                                        Rotating...
                                    {:else}
                                        <RefreshCw class="w-4 h-4" />
                                        Cycle Recovery Key
                                    {/if}
                                </button>
                            </div>
                        {/if}
                    </div>
                </div>
            </div>
        {/if}

        {#if error}
            <div class="bg-rose-50 text-rose-700 p-6 rounded-[2rem] border border-rose-100 flex items-start gap-4" in:fade>
                <AlertCircle class="w-6 h-6 flex-shrink-0" />
                <div>
                    <h4 class="font-black text-sm uppercase tracking-wider">An error occurred</h4>
                    <p class="text-sm font-medium mt-1">{error}</p>
                    <button onclick={() => error = null} class="mt-4 text-xs font-black uppercase tracking-widest border-b border-rose-300 hover:border-rose-600">Dismiss</button>
                </div>
            </div>
        {/if}
    </div>
</div>

<style>
    :global(.glass-card) {
        background: rgba(255, 255, 255, 0.8);
        backdrop-filter: blur(12px);
        -webkit-backdrop-filter: blur(12px);
    }
</style>
