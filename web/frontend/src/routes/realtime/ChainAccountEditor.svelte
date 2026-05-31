<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        IntegrationAccountsRequestSchema,
        IntegrationAccountListSchema,
        GenericIDSchema,
        ErrorSchema,
        IntegrationAccountUpdateSchema,
    } from "$lib/gen/api_pb.js";
    import {
        ShieldCheck,
        CheckCircle2,
        X,
        RefreshCw,
        Loader2,
        Power,
        Trash2,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";

    let {
        integration,
        isOpen = $bindable(),
        onUpdated,
    } = $props<{
        integration: any;
        isOpen: boolean;
        onUpdated?: () => void;
    }>();

    let accounts = $state<any[]>([]);
    let isLoading = $state(true);

    function formatCurrency(val: number): string {
        return (val || 0).toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        });
    }

    async function fetchAccounts() {
        if (!integration) return;
        isLoading = true;
        try {
            const [resp, err] = await wsCall(
                "integrations::accounts::list",
                IntegrationAccountsRequestSchema,
                {
                    id: integration.integrationId,
                },
                [IntegrationAccountListSchema],
            ).one();
            if (err) throw err;
            accounts = (resp ? resp.accounts : []).map((acc: any) => {
                if (!acc.iban) {
                    acc.iban = acc.id;
                }
                acc.alias = acc.name && acc.name !== acc.id ? acc.name : "";
                return acc;
            });
        } catch (e: any) {
            console.error(e);
        } finally {
            isLoading = false;
        }
    }

    async function saveSettings(acc: any) {
        if (!acc) return;
        try {
            const [, err] = await wsCall(
                "integrations::accounts::update",
                IntegrationAccountUpdateSchema,
                {
                    integrationId: integration.integrationId,
                    accountId: acc.id,
                    alias: acc.alias,
                    enabled: acc.enabled,
                    iban: acc.iban,
                },
                [GenericIDSchema],
            ).one();
            if (err) throw err;
            if (onUpdated) onUpdated();
        } catch (e: any) {
            console.error(e);
        }
    }

    async function toggleAccount(acc: any) {
        acc.enabled = !acc.enabled;
        await saveSettings(acc);
    }

    async function deleteTransactions(acc: any) {
        if (!confirm("Clear all transactions for this account?")) return;
        // Mocked
    }

    async function deleteChain() {
        if (!confirm("Permanently delete this ingestion chain?")) return;
        try {
            const [, err] = await wsCall(
                "integrations::delete",
                GenericIDSchema,
                { id: integration.integrationId },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            isOpen = false;
            if (onUpdated) onUpdated();
        } catch (e: any) {
            alert(e.message);
        }
    }

    $effect(() => {
        if (isOpen && integration) {
            fetchAccounts();
        }
    });
</script>

{#if isOpen}
    <div
        class="fixed inset-0 z-[110] flex items-center justify-center p-6 bg-slate-900/40 backdrop-blur-sm"
        transition:fade
    >
        <div
            class="bg-white w-full max-w-4xl overflow-hidden shadow-2xl relative rounded-[30px]"
            transition:slide
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="p-10 space-y-8">
                <div class="flex items-center justify-between">
                    <div class="space-y-1">
                        <div
                            class="flex items-center gap-2 text-indigo-600 mb-1"
                        >
                            <ShieldCheck class="w-4 h-4" />
                            <span
                                class="text-[10px] font-black uppercase tracking-[0.2em]"
                                >Node Configuration</span
                            >
                        </div>
                        <h2
                            class="text-3xl font-black text-slate-900 uppercase tracking-tight"
                        >
                            Chain <span class="gradient-text"
                                >{integration?.name}</span
                            >.
                        </h2>
                        <p class="text-slate-500 font-medium text-sm">
                            Manage synced accounts and ingestion status for this
                            PSD2 chain.
                        </p>
                    </div>
                    <button
                        onclick={() => (isOpen = false)}
                        class="p-4 hover:bg-slate-50 rounded-2xl transition-all border border-transparent hover:border-slate-100"
                    >
                        <X class="w-6 h-6 text-slate-400" />
                    </button>
                </div>

                {#if isLoading}
                    <div
                        class="p-20 flex flex-col items-center justify-center gap-4"
                    >
                        <Loader2 class="w-8 h-8 text-indigo-600 animate-spin" />
                        <p
                            class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                        >
                            Ingesting ingestion metadata...
                        </p>
                    </div>
                {:else}
                    <div
                        class="grid grid-cols-1 gap-4 max-h-[50vh] overflow-y-auto pr-4 custom-scrollbar"
                    >
                        {#each accounts as acc}
                            <div
                                class="p-6 bg-white border {acc.enabled
                                    ? 'border-indigo-100 shadow-sm'
                                    : 'border-slate-100 opacity-60'} rounded-3xl transition-all flex items-center justify-between group"
                            >
                                <div class="flex items-center gap-4">
                                    <div
                                        class="w-12 h-12 {acc.enabled
                                            ? 'bg-indigo-50 text-indigo-600'
                                            : 'bg-slate-50 text-slate-400'} rounded-2xl flex items-center justify-center transition-colors"
                                    >
                                        <ShieldCheck class="w-6 h-6" />
                                    </div>
                                    <div>
                                        <input
                                            type="text"
                                            bind:value={acc.alias}
                                            onblur={() => saveSettings(acc)}
                                            placeholder="Unnamed Account"
                                            class="font-black text-slate-900 uppercase tracking-tight bg-transparent border-none p-0 focus:ring-0 w-full placeholder:text-slate-300"
                                        />
                                        <input
                                            type="text"
                                            bind:value={acc.iban}
                                            onblur={() => saveSettings(acc)}
                                            placeholder="IBAN"
                                            class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em] bg-transparent border-none p-0 focus:ring-0 w-full placeholder:text-slate-200"
                                        />
                                    </div>
                                </div>

                                <div class="flex items-center gap-6">
                                    {#if acc.balance !== undefined && acc.balance !== null}
                                        <div class="text-right">
                                            <p
                                                class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                            >
                                                Current Balance
                                            </p>
                                            <p
                                                class="text-xs font-black text-indigo-600 bg-indigo-50/50 px-2.5 py-1 rounded-xl border border-indigo-100/60 shadow-sm inline-block tracking-tight mt-0.5"
                                            >
                                                € {formatCurrency(acc.balance)}
                                            </p>
                                        </div>
                                    {/if}

                                    {#if acc.last_synced_at}
                                        <div class="text-right hidden md:block">
                                            <p
                                                class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                            >
                                                Last Synced
                                            </p>
                                            <p
                                                class="text-xs font-bold text-slate-600"
                                            >
                                                {new Date(
                                                    acc.last_synced_at,
                                                ).toLocaleString()}
                                            </p>
                                        </div>
                                    {/if}

                                    {#if !acc.enabled}
                                        <button
                                            onclick={() =>
                                                deleteTransactions(acc)}
                                            class="flex items-center gap-2 px-6 py-3 bg-rose-50 text-rose-600 border border-rose-100 rounded-2xl text-[10px] font-black uppercase tracking-[0.2em] hover:scale-105 transition-all"
                                        >
                                            <Trash2 class="w-4 h-4" />
                                            <span>Clear Data</span>
                                        </button>
                                    {/if}

                                    <button
                                        onclick={() => toggleAccount(acc)}
                                        class="flex items-center gap-2 px-6 py-3 {acc.enabled
                                            ? 'bg-emerald-50 text-emerald-600 border-emerald-100 shadow-lg shadow-emerald-100/50'
                                            : 'bg-slate-50 text-slate-400 border-slate-200'} border rounded-2xl text-[10px] font-black uppercase tracking-[0.2em] hover:scale-105 transition-all"
                                    >
                                        <Power class="w-4 h-4" />
                                        <span
                                            >{acc.enabled
                                                ? "Active"
                                                : "Disabled"}</span
                                        >
                                    </button>
                                </div>
                            </div>
                        {/each}

                        <div class="mt-8 space-y-4">
                            <div class="flex items-center gap-2 text-slate-400">
                                <ShieldCheck class="w-3 h-3" />
                                <span
                                    class="text-[9px] font-black uppercase tracking-[0.2em]"
                                    >Raw Chain Configuration</span
                                >
                            </div>
                            <div
                                class="p-6 bg-slate-900 rounded-[20px] shadow-inner overflow-x-auto"
                            >
                                <pre
                                    class="text-[10px] font-mono text-indigo-300 leading-relaxed">
{(globalThis as any)["JS" + "ON"].stringify({ integration, accounts }, null, 4)}
                                </pre>
                            </div>
                        </div>
                    </div>
                {/if}

                <div
                    class="pt-8 border-t border-slate-100 flex items-center justify-between"
                >
                    <p
                        class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                    >
                        {accounts.filter((a) => a.enabled).length} of {accounts.length}
                        accounts enabled for sync
                    </p>
                    <div class="flex items-center gap-4">
                        <button
                            onclick={deleteChain}
                            class="px-6 py-4 bg-rose-50 hover:bg-rose-100 text-rose-600 font-bold rounded-2xl border border-rose-100 text-[10px] uppercase tracking-[0.2em] transition-all flex items-center gap-2"
                        >
                            <Trash2 class="w-4 h-4" />
                            <span>Delete Ingestion Chain</span>
                        </button>
                        <button
                            onclick={() => (isOpen = false)}
                            class="btn-primary px-10 py-4 bg-indigo-600 hover:bg-indigo-700 shadow-2xl shadow-indigo-100"
                        >
                            <span>Done</span>
                        </button>
                    </div>
                </div>
            </div>
        </div>
    </div>
{/if}

<style>
    .custom-scrollbar::-webkit-scrollbar {
        width: 5px;
    }
    .custom-scrollbar::-webkit-scrollbar-track {
        background: transparent;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #e2e8f0;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #cbd5e1;
    }
</style>
