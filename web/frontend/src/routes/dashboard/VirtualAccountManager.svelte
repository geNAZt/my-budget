<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        VirtualAccountListSchema,
        VirtualAccountSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    const decode = (obj: any) => {
        if (!obj) return obj;
        try {
            return structuredClone(obj);
        } catch (e) {
            return JSON.parse(JSON.stringify(obj));
        }
    };

    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Edit3,
        X,
        Check,
        Save,
        RotateCcw,
        AlertTriangle,
        ArrowRight,
        Briefcase,
        Wallet,
        Calendar,
        FileText,
        Info,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";

    interface VirtualAccountVersion {
        id?: string;
        virtualAccountId?: string;
        color: string;
        startingBalance: number;
        description: string;
        createdAt?: string;
    }

    interface VirtualAccount {
        id?: string;
        name: string;
        activeVersion?: VirtualAccountVersion;
    }

    let accounts = $state<(VirtualAccount & { activeVersion: VirtualAccountVersion })[]>([]);
    let sortedAccounts = $derived(
        [...accounts].sort((a, b) => {
            return (a.name || "").localeCompare(b.name || "");
        })
    );
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let currentAccount = $state<VirtualAccount & { activeVersion: VirtualAccountVersion }>(createNewAccount() as any);
    let balanceInput = $state("");
    let accountToDelete = $state<string | null>(null);

    const PRESET_COLORS = [
        "#6366f1", // Indigo
        "#8b5cf6", // Violet
        "#ec4899", // Pink
        "#f43f5e", // Rose
        "#06b6d4", // Cyan
        "#10b981", // Emerald
        "#f59e0b", // Amber
        "#3b82f6", // Blue
    ];

    function createNewAccount(): VirtualAccount & { activeVersion: VirtualAccountVersion } {
        return {
            name: "",
            activeVersion: {
                color: "#6366f1",
                startingBalance: 0,
                description: "",
            },
        } as any;
    }

    async function fetchAccounts() {
        isLoading = true;
        error = null;
        try {
            const [resp, err] = await wsCall(
                "virtualaccounts::list",
                null,
                null,
                [VirtualAccountListSchema],
            ).one();
            if (err) throw err;
            accounts = resp.virtualAccounts as any;
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    function formatGermanAmount(val: number) {
        return val.toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        });
    }

    function parseGermanAmount(str: string): number {
        const normalized = str.replace(/\./g, "").replace(",", ".");
        return parseFloat(normalized) || 0;
    }

    function openAdd() {
        currentAccount = createNewAccount();
        balanceInput = "0,00";
        showAddModal = true;
    }

    function openEdit(acc: VirtualAccount) {
        currentAccount = decode(acc);
        if (!currentAccount.activeVersion) {
            currentAccount.activeVersion = {
                color: "#6366f1",
                startingBalance: 0,
                description: "",
            };
        }
        balanceInput = formatGermanAmount(
            currentAccount.activeVersion.startingBalance,
        );
        showAddModal = true;
    }

    async function handleSave() {
        if (!currentAccount.name) return;

        isSaving = true;
        try {
            if (!currentAccount.activeVersion) {
                currentAccount.activeVersion = {
                    color: "#6366f1",
                    startingBalance: 0,
                    description: "",
                };
            }
            currentAccount.activeVersion.startingBalance =
                parseGermanAmount(balanceInput);
            await wsCall(
                "virtualaccounts::save",
                VirtualAccountSchema,
                {
                    id: currentAccount.id || "",
                    name: currentAccount.name,
                    activeVersion: {
                        id: currentAccount.activeVersion.id || "",
                        virtualAccountId:
                            currentAccount.activeVersion.virtualAccountId || "",
                        color: currentAccount.activeVersion.color,
                        startingBalance:
                            currentAccount.activeVersion.startingBalance,
                        description: currentAccount.activeVersion.description,
                    },
                },
                [VirtualAccountSchema],
            ).one();
            await fetchAccounts();
            showAddModal = false;
        } catch (err: any) {
            error = err.message;
        } finally {
            isSaving = false;
        }
    }

    function triggerDelete(id: string) {
        accountToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "archive") {
        if (!accountToDelete) return;
        try {
            await wsCall(
                "virtualaccounts::delete",
                GenericIDSchema,
                {
                    id: accountToDelete,
                },
                [ErrorSchema],
            ).one();
            await fetchAccounts();
            showDeleteConfirm = false;
            accountToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
    }

    onMount(() => {
        fetchAccounts();
    });
</script>

<svelte:window onkeydown={(e) => {
    if (e.key === 'Escape') {
        showAddModal = false;
        showDeleteConfirm = false;
    }
}} />

<div class="space-y-8">
    <!-- Header -->
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div>
            <h1 class="text-4xl font-black text-slate-900 tracking-tight">
                Virtual Accounts
            </h1>
            <p class="text-slate-500 font-medium text-sm mt-1">
                Deterministic account structures to model planned financial
                distributions and shift plans.
            </p>
        </div>
        <button
            onclick={openAdd}
            class="btn-primary flex items-center justify-center gap-2 py-3 px-6 !bg-indigo-600 !border-indigo-600 self-start md:self-auto"
        >
            <Plus class="w-4 h-4" />
            <span>Create Account</span>
        </button>
    </div>

    <!-- Error state -->
    {#if error}
        <div
            class="p-6 bg-rose-50 text-rose-600 rounded-[20px] font-bold border border-rose-100 flex items-center gap-3"
        >
            <AlertTriangle class="w-5 h-5 flex-shrink-0" />
            <span>{error}</span>
        </div>
    {/if}

    {#if isLoading}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each Array(3) as _}
                <div
                    class="h-48 bg-slate-100 rounded-[32px] animate-pulse"
                ></div>
            {/each}
        </div>
    {:else if accounts.length === 0}
        <div
            class="flex flex-col items-center justify-center p-12 bg-slate-50 border-2 border-dashed border-slate-200 rounded-[32px] text-center"
        >
            <div
                class="w-16 h-16 bg-white rounded-2xl shadow-sm flex items-center justify-center text-slate-400 mb-4"
            >
                <Briefcase class="w-8 h-8" />
            </div>
            <h3 class="text-lg font-black text-slate-900">
                No virtual accounts yet
            </h3>
            <p class="text-slate-500 max-w-sm mt-2 font-medium">
                Create your first virtual account to start organizing your
                budget into deterministic pools.
            </p>
            <button
                onclick={openAdd}
                class="btn-primary !bg-indigo-600 !border-indigo-600 font-black uppercase text-[10px] tracking-[0.2em] px-6 py-3.5 mt-2"
            >
                Create First Account
            </button>
        </div>
    {:else}
        <div class="glass-card overflow-hidden">
            <div class="overflow-x-auto">
                <table class="w-full border-collapse text-left">
                    <thead>
                        <tr class="border-b border-slate-100 bg-slate-50/50">
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 w-12">Color</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Name</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Description</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Type</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Starting Balance</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each sortedAccounts as acc (acc.id)}
                            <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                                <td class="px-6 py-4">
                                    <div
                                        class="w-6 h-6 rounded-lg border border-white/20 shadow-sm"
                                        style="background-color: {acc.activeVersion?.color || '#cbd5e1'}"
                                    ></div>
                                </td>
                                <td class="px-6 py-4 font-bold text-slate-800">{acc.name}</td>
                                <td class="px-6 py-4 text-xs font-bold text-slate-700 max-w-xs truncate">
                                    {acc.activeVersion?.description || "No description provided."}
                                </td>
                                <td class="px-6 py-4 text-xs font-bold text-slate-700">
                                    <span class="px-2 py-0.5 bg-slate-100 text-slate-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]">
                                        Virtual
                                    </span>
                                </td>
                                <td class="px-6 py-4">
                                    <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900">
                                        <span>€</span>
                                        <span>{formatGermanAmount(acc.activeVersion?.startingBalance)}</span>
                                    </div>
                                </td>
                                <td class="px-6 py-4 text-right">
                                    <div class="inline-flex gap-2">
                                        <button
                                            onclick={() => openEdit(acc)}
                                            class="p-2 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 border border-transparent hover:border-indigo-100 rounded-xl transition-all"
                                            title="Edit"
                                        >
                                            <Edit3 class="w-4 h-4" />
                                        </button>
                                        <button
                                            onclick={() => triggerDelete(acc.id!)}
                                            class="p-2 text-slate-400 hover:text-rose-600 hover:bg-rose-50 border border-transparent hover:border-rose-100 rounded-xl transition-all"
                                            title="Delete"
                                        >
                                            <Trash2 class="w-4 h-4" />
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        </div>
    {/if}
</div>

<!-- Add/Edit Modal -->
{#if showAddModal}
    <div
        class="fixed inset-0 bg-slate-900/40 backdrop-blur-md z-[100] flex items-center justify-center p-4"
        transition:fade
    >
        <div
            class="bg-white rounded-[32px] shadow-2xl w-full max-w-xl overflow-hidden"
            transition:slide
        >
            <div
                class="p-8 border-b border-slate-100 flex items-center justify-between bg-slate-50/50"
            >
                <div>
                    <h2
                        class="text-2xl font-black text-slate-900 tracking-tight"
                    >
                        {currentAccount.id
                            ? "Edit Account"
                            : "New Virtual Account"}
                    </h2>
                    <p class="text-slate-500 text-sm font-medium">
                        Define your logical budget structure.
                    </p>
                </div>
                <button
                    onclick={() => (showAddModal = false)}
                    class="p-2 hover:bg-white hover:shadow-md rounded-xl transition-all text-slate-400 hover:text-slate-900"
                >
                    <X class="w-6 h-6" />
                </button>
            </div>

            <div class="p-8 space-y-6">
                <div class="space-y-2">
                    <label
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block"
                        >Account Name</label
                    >
                    <input
                        type="text"
                        bind:value={currentAccount.name}
                        placeholder="e.g. Daily Operations, Tax Reserve..."
                        class="w-full px-5 py-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-slate-900"
                    />
                </div>

                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div class="space-y-2">
                        <label
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block"
                            >Starting Balance (€)</label
                        >
                        <input
                            type="text"
                            bind:value={balanceInput}
                            class="w-full px-5 py-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-slate-900"
                        />
                    </div>
                    <div class="space-y-2">
                        <label
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block"
                            >Custom Color</label
                        >
                        <div
                            class="flex items-center gap-3 h-[58px] border border-slate-200 px-3.5 rounded-2xl bg-slate-50"
                        >
                            <input
                                type="color"
                                bind:value={currentAccount.activeVersion.color}
                                class="w-10 h-10 border-none bg-transparent cursor-pointer"
                            />
                            <span
                                class="font-mono text-sm font-bold text-slate-500 uppercase"
                                >{currentAccount.activeVersion.color}</span
                            >
                        </div>
                    </div>
                </div>

                <div class="space-y-2">
                    <label
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block"
                        >Preselected Themes</label
                    >
                    <div class="flex flex-wrap gap-2.5">
                        {#each PRESET_COLORS as color}
                            <button
                                onclick={() =>
                                    (currentAccount.activeVersion.color =
                                        color)}
                                class="w-8 h-8 rounded-full border-2 transition-all {currentAccount
                                    .activeVersion.color === color
                                    ? 'border-indigo-600 scale-110 shadow-lg'
                                    : 'border-transparent hover:scale-105'}"
                                style="background-color: {color}"
                            ></button>
                        {/each}
                    </div>
                </div>

                <div class="space-y-2">
                    <label
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block"
                        >Strategic Description</label
                    >
                    <textarea
                        bind:value={currentAccount.activeVersion.description}
                        placeholder="What is the purpose of this account?"
                        rows="3"
                        class="w-full px-5 py-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-slate-900 resize-none"
                    ></textarea>
                </div>
            </div>

            <div
                class="p-8 bg-slate-50/50 border-t border-slate-100 flex gap-3"
            >
                <button
                    onclick={() => (showAddModal = false)}
                    class="flex-1 py-4 bg-white border border-slate-200 text-slate-600 rounded-2xl font-black uppercase tracking-[0.2em] text-[10px] hover:bg-slate-100 transition-all active:scale-95"
                >
                    Discard Changes
                </button>
                <button
                    onclick={handleSave}
                    disabled={isSaving}
                    class="flex-[2] py-4 bg-slate-900 text-white rounded-2xl font-black uppercase tracking-[0.2em] text-[10px] hover:bg-indigo-600 transition-all active:scale-95 disabled:opacity-50 flex items-center justify-center gap-2"
                >
                    {#if isSaving}
                        <RotateCcw class="w-4 h-4 animate-spin" />
                    {:else}
                        <Save class="w-4 h-4" />
                    {/if}
                    {currentAccount.id ? "Update Account" : "Persist Account"}
                </button>
            </div>
        </div>
    </div>
{/if}

<!-- Delete Confirmation Modal -->
{#if showDeleteConfirm}
    <div
        class="fixed inset-0 bg-slate-900/40 backdrop-blur-md z-[110] flex items-center justify-center p-4"
        transition:fade
    >
        <div
            class="bg-white rounded-[32px] shadow-2xl w-full max-w-md overflow-hidden p-8"
            transition:slide
        >
            <div
                class="w-16 h-16 bg-rose-50 rounded-2xl flex items-center justify-center text-rose-600 mb-6"
            >
                <Trash2 class="w-8 h-8" />
            </div>

            <h2 class="text-2xl font-black text-slate-900 tracking-tight">
                Destructive Action
            </h2>
            <p class="text-slate-500 font-medium mt-2">
                How should we handle the removal of this virtual account and its
                historical data?
            </p>

            <div class="space-y-3 mt-8">
                <button
                    onclick={() => confirmDelete("revert")}
                    class="w-full text-left p-6 border border-slate-200 rounded-2xl hover:border-amber-400 hover:bg-amber-50/20 transition-all flex items-start gap-4 group"
                >
                    <div
                        class="w-10 h-10 bg-amber-50 rounded-xl flex items-center justify-center text-amber-600 group-hover:bg-amber-100 transition-colors"
                    >
                        <RotateCcw class="w-5 h-5" />
                    </div>
                    <div>
                        <span
                            class="block font-black text-slate-900 uppercase tracking-wider text-xs"
                            >Revert Changes</span
                        >
                        <span class="text-xs text-slate-500 font-medium"
                            >Remove the account but keep past projections
                            unaffected.</span
                        >
                    </div>
                </button>

                <button
                    onclick={() => confirmDelete("archive")}
                    class="w-full text-left p-6 border border-slate-200 rounded-2xl hover:border-rose-400 hover:bg-rose-50/20 transition-all flex items-start gap-4 group"
                >
                    <div
                        class="w-10 h-10 bg-rose-50 rounded-xl flex items-center justify-center text-rose-600 group-hover:bg-rose-100 transition-colors"
                    >
                        <Trash2 class="w-5 h-5" />
                    </div>
                    <div>
                        <span
                            class="block font-black text-slate-900 uppercase tracking-wider text-xs"
                            >Full Purge</span
                        >
                        <span class="text-xs text-slate-500 font-medium"
                            >Completely remove the account and all associated
                            data.</span
                        >
                    </div>
                </button>
            </div>

            <button
                onclick={() => (showDeleteConfirm = false)}
                class="w-full mt-8 py-3 text-slate-400 font-black uppercase tracking-[0.2em] text-[10px] hover:text-slate-900 transition-colors"
            >
                Cancel Action
            </button>
        </div>
    </div>
{/if}

<style>
    @reference "../../app.css";
    .glass-card {
        @apply bg-white border border-slate-100 rounded-[32px] shadow-sm;
    }

    .btn-primary {
        @apply px-6 py-3 bg-slate-900 text-white rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] hover:bg-indigo-600 transition-all active:scale-95 disabled:opacity-50;
    }
</style>
