<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
    import {
        VirtualAccountListSchema,
        VirtualAccountSchema,
        GenericIDSchema,
        ErrorSchema,
        IntegrationAccountListSchema,
    } from "$lib/gen/api_pb.js";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import { formatGermanAmount } from "$lib/utils/format";


    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Pencil,
        RotateCcw,
        AlertTriangle,
        Wallet,
        Briefcase,
    } from "@lucide/svelte";
    import { fade } from "svelte/transition";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";
    import CurrencyInput from "$lib/components/ui/CurrencyInput.svelte";
    import Badge from "$lib/components/ui/Badge.svelte";
    import Modal from "$lib/components/ui/Modal.svelte";
    import ConfirmModal from "$lib/components/ui/ConfirmModal.svelte";
    import Table from "$lib/components/ui/Table.svelte";

    interface VirtualAccountVersion {
        id?: string;
        virtualAccountId?: string;
        color: string;
        startingBalance: number;
        description: string;
        realtimeAccountId?: string;
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
    let realtimeAccounts = $state<{ id: string; name: string; balance: number }[]>([]);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let currentAccount = $state<VirtualAccount & { activeVersion: VirtualAccountVersion }>(createNewAccount() as any);
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
                realtimeAccountId: "",
            },
        } as any;
    }

    async function fetchRealtimeAccounts() {
        try {
            const [resp, err] = await wsCall(
                "integrations::accounts::list",
                null,
                null,
                [IntegrationAccountListSchema],
            ).one();
            if (!err && resp && resp.accounts) {
                realtimeAccounts = resp.accounts.map((a: any) => ({
                    id: a.id,
                    name: a.name || a.id,
                    balance: a.balance || 0,
                }));
            }
        } catch (e) {
            console.error("Failed to fetch realtime accounts", e);
        }
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



    function openAdd() {
        currentAccount = createNewAccount();
        showAddModal = true;
    }

    function openEdit(acc: VirtualAccount) {
        currentAccount = decode(acc);
        if (!currentAccount.activeVersion) {
            currentAccount.activeVersion = {
                color: "#6366f1",
                startingBalance: 0,
                description: "",
                realtimeAccountId: "",
            };
        } else {
            if (currentAccount.activeVersion.realtimeAccountId === undefined) {
                currentAccount.activeVersion.realtimeAccountId = "";
            }
            currentAccount.activeVersion.startingBalance = currentAccount.activeVersion.startingBalance ?? 0;
        }
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
                    realtimeAccountId: "",
                };
            }
            const [, err] = await wsCall(
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
                        realtimeAccountId: currentAccount.activeVersion.realtimeAccountId || "",
                    },
                },
                [VirtualAccountSchema, ErrorSchema],
            ).one();
            if (err) throw err;
            showAddModal = false;
            await fetchAccounts();
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
            const [, err] = await wsCall(
                "virtualaccounts::delete",
                GenericIDSchema,
                { id: accountToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchAccounts();
            showDeleteConfirm = false;
            accountToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
    }

    onMount(() => {
        fetchAccounts();
        fetchRealtimeAccounts();
    });
</script>

<div class="space-y-8">
    <!-- Header -->
    <div class="flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div>
            <h2 class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500">
                Virtual Accounts
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Logical allocation nodes for managing budget boundaries.
            </p>
        </div>
        <div>
            <Button onclick={openAdd}>
                <Plus class="w-5 h-5" />
                Add Account
            </Button>
        </div>
    </div>

    {#if error}
        <div
            transition:fade
            class="glass-card p-6 border-rose-200 bg-rose-50/50 flex items-center gap-4 text-rose-600"
        >
            <AlertTriangle class="w-6 h-6 flex-shrink-0" />
            <div class="flex-1">
                <p class="text-xs font-black uppercase tracking-widest">
                    Connection Error
                </p>
                <p class="text-sm font-bold">{error}</p>
            </div>
            <Button
                onclick={fetchAccounts}
                class="bg-rose-600 text-white hover:bg-rose-700 shadow-rose-200"
            >
                Retry
            </Button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <div class="w-10 h-10 border-4 border-t-indigo-600 border-indigo-100 rounded-full animate-spin"></div>
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Syncing Accounts...
            </p>
        </div>
    {:else if accounts.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <Wallet class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Virtual Accounts
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Create allocation nodes like Savings, Taxes, or Rent to group transactions.
                </p>
            </div>
            <Button variant="secondary" onclick={openAdd} class="mx-auto">
                Create First Account
            </Button>
        </div>
    {:else}
        <Table>
            {#snippet header()}
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Account</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Realtime Link</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Description</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Balance</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedAccounts as acc (acc.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4">
                            <div class="flex items-center gap-3">
                                <div
                                    class="w-3.5 h-3.5 rounded-full border border-black/5 flex-shrink-0"
                                    style="background-color: {acc.activeVersion?.color || '#cbd5e1'}"
                                ></div>
                                <span class="font-bold text-slate-800 dark:text-slate-100">{acc.name}</span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            {#if acc.activeVersion?.realtimeAccountId}
                                <Badge variant="success">Connected</Badge>
                            {:else}
                                <Badge variant="slate">Static Only</Badge>
                            {/if}
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-500 max-w-xs truncate">
                            {acc.activeVersion?.description || "—"}
                        </td>
                        <td class="px-6 py-4">
                            <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900 dark:text-slate-100">
                                <span>€</span>
                                <span>{formatGermanAmount(acc.activeVersion?.startingBalance || 0)}</span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => openEdit(acc)}
                                    title="Edit Account"
                                    class="hover:text-indigo-600 hover:bg-indigo-50 hover:border-indigo-100"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => triggerDelete(acc.id!)}
                                    title="Archive Account"
                                    class="hover:text-red-600 hover:bg-red-50 hover:border-red-100"
                                >
                                    <Trash2 class="w-4 h-4" />
                                </Button>
                            </div>
                        </td>
                    </tr>
                {/each}
            {/snippet}
        </Table>
    {/if}
</div>

<!-- Add/Edit Modal -->
<Modal
    bind:open={showAddModal}
    title={currentAccount.id ? 'Edit Account' : 'New Virtual Account'}
    subtitle="Define your logical budget structure."
>
    {#if currentAccount.activeVersion}
        <form
            onsubmit={(e) => {
                e.preventDefault();
                handleSave();
            }}
            class="space-y-6"
        >
            <Input
                label="Account Name"
                bind:value={currentAccount.name}
                placeholder="e.g. Daily Operations, Tax Reserve..."
                required
            />

            <SearchableDropdown
                label="Link Realtime Account"
                options={[
                    { id: "", label: "No Link (Static Starting Balance)" },
                    ...realtimeAccounts.map(acc => ({
                        id: acc.id,
                        label: `${acc.name} (${formatGermanAmount(acc.balance)} €)`
                    }))
                ]}
                bind:value={currentAccount.activeVersion.realtimeAccountId}
                placeholder="Select integrated account..."
            />

            <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                <CurrencyInput
                    label="Starting Balance (€)"
                    bind:value={currentAccount.activeVersion.startingBalance}
                    disabled={!!currentAccount.activeVersion.realtimeAccountId}
                />
                <div class="space-y-2">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                        Custom Color
                    </label>
                    <div class="flex items-center gap-3 h-[50px] border border-slate-200 px-3.5 rounded-2xl bg-white dark:bg-slate-800 dark:border-slate-700">
                        <input
                            type="color"
                            bind:value={currentAccount.activeVersion.color}
                            class="w-8 h-8 border-none bg-transparent cursor-pointer"
                        />
                        <span class="font-mono text-sm font-bold text-slate-500 dark:text-slate-400 uppercase">
                            {currentAccount.activeVersion.color}
                        </span>
                    </div>
                </div>
            </div>

            <div class="space-y-2">
                <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                    Preselected Themes
                </label>
                <div class="flex flex-wrap gap-2.5">
                    {#each PRESET_COLORS as color}
                        <button
                            type="button"
                            onclick={() => (currentAccount.activeVersion.color = color)}
                            class="w-8 h-8 rounded-full border-2 transition-all {currentAccount.activeVersion.color === color ? 'border-indigo-600 scale-110 shadow-lg' : 'border-transparent hover:scale-105'}"
                            style="background-color: {color}"
                        ></button>
                    {/each}
                </div>
            </div>

            <div class="space-y-2">
                <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                    Strategic Description
                </label>
                <textarea
                    bind:value={currentAccount.activeVersion.description}
                    placeholder="What is the purpose of this account?"
                    rows="3"
                    class="w-full px-5 py-4 bg-white border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-slate-900 resize-none dark:bg-slate-800 dark:border-slate-700 dark:text-slate-100"
                ></textarea>
            </div>

            <div class="pt-4 flex gap-3">
                <Button
                    variant="secondary"
                    onclick={() => (showAddModal = false)}
                    class="flex-1 py-4 text-xs tracking-wider"
                >
                    Discard Changes
                </Button>
                <Button
                    type="submit"
                    variant="primary"
                    loading={isSaving}
                    loadingLabel="Persisting..."
                    class="flex-[2] py-4 text-xs tracking-wider"
                >
                    {currentAccount.id ? "Update Account" : "Persist Account"}
                </Button>
            </div>
        </form>
    {/if}
</Modal>

<!-- Delete Confirmation Modal -->
<ConfirmModal
    bind:open={showDeleteConfirm}
    title="Destructive Action"
    description="How should we handle the removal of this virtual account and its historical data?"
>
    <div class="space-y-3">
        <button
            onclick={() => confirmDelete("revert")}
            class="w-full text-left p-6 border border-slate-200 rounded-2xl hover:border-amber-400 hover:bg-amber-50/20 transition-all flex items-start gap-4 group dark:border-slate-800 dark:hover:bg-slate-800/50"
        >
            <div class="w-10 h-10 bg-amber-50 dark:bg-amber-500/20 rounded-xl flex items-center justify-center text-amber-600 dark:text-amber-400 group-hover:bg-amber-100 transition-colors">
                <RotateCcw class="w-5 h-5" />
            </div>
            <div>
                <span class="block font-black text-slate-900 dark:text-slate-100 uppercase tracking-wider text-xs">
                    Revert Changes
                </span>
                <span class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Remove the account but keep past projections unaffected.
                </span>
            </div>
        </button>

        <button
            onclick={() => confirmDelete("archive")}
            class="w-full text-left p-6 border border-slate-200 rounded-2xl hover:border-rose-400 hover:bg-rose-50/20 transition-all flex items-start gap-4 group dark:border-slate-800 dark:hover:bg-slate-800/50"
        >
            <div class="w-10 h-10 bg-rose-50 dark:bg-rose-500/20 rounded-xl flex items-center justify-center text-rose-600 dark:text-rose-400 group-hover:bg-rose-100 transition-colors">
                <Trash2 class="w-5 h-5" />
            </div>
            <div>
                <span class="block font-black text-slate-900 dark:text-slate-100 uppercase tracking-wider text-xs">
                    Full Purge
                </span>
                <span class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Completely remove the account and all associated data.
                </span>
            </div>
        </button>
    </div>

    <Button
        variant="secondary"
        onclick={() => (showDeleteConfirm = false)}
        class="mt-8 w-full"
    >
        Cancel Action
    </Button>
</ConfirmModal>
