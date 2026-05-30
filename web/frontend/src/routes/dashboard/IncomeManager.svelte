<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        IncomeListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        ScenarioListSchema,
        ModificationListSchema,
        IncomeSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
    const decode = (obj: any) => JSON.parse(JSON.stringify(obj));

    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Calendar,
        Euro,
        ArrowRight,
        Clock,
        Loader2,
        History,
        Archive,
        Undo2,
        Pencil,
        ClipboardPaste,
        CheckCircle2,
        AlertCircle,
        Check,
        Globe,
        Languages,
        Wallet,
        Layers,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";

    interface IncomeVersion {
        amount: number;
        stopModificationId: string | null;
        startDate: string;
        endDate: string | null;
        intervalMonths: number;
    }

    interface Income {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        activeVersion?: IncomeVersion;
        import_selected?: boolean;
        linkToScenarios?: string[]; // Selection for new entities
    }

    let incomes = $state<Income[]>([]);
    let pools = $state<any[]>([]);
    let virtualAccounts = $state<any[]>([]);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    // Context for linking
    let scenarios = $state<any[]>([]);
    let modifications = $state<any[]>([]);

    const poolOptions = $derived([
        { id: "", label: "None / Uncategorized" },
        ...(pools || []).map((p) => ({
            id: p.id,
            label: p.name,
        })),
    ]);

    const virtualAccountOptions = $derived([
        { id: "", label: "None / General" },
        ...(virtualAccounts || []).map((va) => ({
            id: va.id,
            label: va.name,
        })),
    ]);

    const virtualAccountMultiOptions = $derived(
        (virtualAccounts || []).map((va) => ({
            id: va.id,
            label: va.name,
        })),
    );

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let showImportModal = $state(false);
    let currentIncome = $state<Income>(createNewIncome());
    let amountInput = $state("");
    let incomeToDelete = $state<string | null>(null);

    // Import State
    let rawImportData = $state("");
    let previewIncomes = $state<Income[]>([]);
    let importLocale = $state<"DE" | "US">("US");

    function createNewIncome(): Income {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                amount: 0,
                stopModificationId: null,
                startDate: monthStr,
                endDate: null,
                intervalMonths: 1,
            },
            linkToScenarios: [],
        };
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [iR, pR, vaR, sR, mR] = await Promise.all([
                wsCall("incomes::list", null, null, [IncomeListSchema]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
                wsCall("scenarios::list", null, null, [
                    ScenarioListSchema,
                ]).one(),
                wsCall("modifications::list", null, null, [
                    ModificationListSchema,
                ]).one(),
            ]);

            if (iR[1]) throw iR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];
            if (sR[1]) throw sR[1];
            if (mR[1]) throw mR[1];

            incomes = iR[0].incomes;
            pools = pR[0].pools;
            virtualAccounts = vaR[0].virtualAccounts;
            scenarios = sR[0].scenarios;
            modifications = mR[0].modifications;
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function saveIncome() {
        if (!currentIncome.name) return;
        isSaving = true;
        try {
            currentIncome.activeVersion.amount = parseGermanAmount(amountInput);
            const [, err] = await wsCall(
                "incomes::save",
                IncomeSchema,
                {
                    id: currentIncome.id || "",
                    name: currentIncome.name,
                    poolId: currentIncome.poolId || "",
                    accountIds: currentIncome.accountIds || [],
                    activeVersion: {
                        id: currentIncome.activeVersion.id || "",
                        incomeId: currentIncome.activeVersion.incomeId || "",
                        amount: currentIncome.activeVersion.amount || 0,
                        stopModificationId:
                            currentIncome.activeVersion.stopModificationId ||
                            "",
                        startDate: currentIncome.activeVersion.startDate || "",
                        endDate: currentIncome.activeVersion.endDate || "",
                        intervalMonths:
                            currentIncome.activeVersion.intervalMonths || 1,
                        createdAt: currentIncome.activeVersion.createdAt || "",
                    },
                    linkToScenarios: currentIncome.linkToScenarios || [],
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showAddModal = false;
            await fetchData();
        } catch (err: any) {
            error = err.message;
        } finally {
            isSaving = false;
        }
    }

    function editIncome(income: Income) {
        currentIncome = decode(income);
        if (!currentIncome.activeVersion) {
            currentIncome.activeVersion = {
                amount: 0,
                stopModificationId: null,
                startDate: new Date().toISOString(),
                endDate: null,
                intervalMonths: 1,
            };
        }
        amountInput = formatGermanAmount(currentIncome.activeVersion.amount);
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        incomeToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "full") {
        if (!incomeToDelete) return;
        try {
            const [, err] = await wsCall(
                "incomes::delete",
                GenericIDSchema,
                { id: incomeToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            incomeToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
    }

    async function executeImport() {
        const toImport = previewIncomes.filter((i) => i.import_selected);
        if (toImport.length === 0) return;

        try {
            isSaving = true;
            const [, err] = await wsCall(
                "incomes::save_bulk",
                IncomeListSchema,
                {
                    incomes: toImport.map((i) => ({
                        id: i.id || "",
                        name: i.name,
                        poolId: i.poolId || "",
                        accountIds: i.accountIds || [],
                        activeVersion: i.activeVersion
                            ? {
                                  id: i.activeVersion.id || "",
                                  incomeId: i.activeVersion.incomeId || "",
                                  amount: i.activeVersion.amount || 0,
                                  stopModificationId:
                                      i.activeVersion.stopModificationId || "",
                                  startDate: i.activeVersion.startDate || "",
                                  endDate: i.activeVersion.endDate || "",
                                  intervalMonths:
                                      i.activeVersion.intervalMonths || 1,
                                  createdAt: i.activeVersion.createdAt || "",
                              }
                            : undefined,
                        linkToScenarios: i.linkToScenarios || [],
                    })),
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showImportModal = false;
            rawImportData = "";
            previewIncomes = [];
            await fetchData();
        } catch (err: any) {
            alert(err.message);
        } finally {
            isSaving = false;
        }
    }

    function parseNumericAmount(val: string | number, locale: "DE" | "US"): number {
        if (typeof val === "number") return val; if (!val) return 0;
        if (locale === "DE") return parseGermanAmount(val);
        let clean = val.toString().trim().replace(/,/g, "");
        return parseFloat(clean) || 0;
    }

    function parseDateString(val: string): string {
        if (!val || val.trim() === "") {
            const now = new Date();
            return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;
        }
        const parts = val.split("/");
        if (parts.length === 2) {
            const month = parts[0].padStart(2, "0");
            const year = parts[1];
            return `${year}-${month}-01T00:00:00Z`;
        }
        return new Date().toISOString();
    }

    $effect(() => {
        if (rawImportData.trim() === "") {
            previewIncomes = [];
            return;
        }

        const lines = rawImportData.trim().split("\n");
        const detected: Income[] = [];

        lines.forEach((line) => {
            const parts = line.split("\t");
            if (parts.length < 2) return;

            const name = parts[0].trim();
            const amount = parseNumericAmount(parts[1], importLocale);
            const interval = parseInt(parts[2]) || 1;
            const startDate = parseDateString(parts[3]);
            const endDate = parts[4] ? parseDateString(parts[4]) : null;

            detected.push({
                name,
                poolId: null,
                accountIds: [],
                import_selected: true,
                activeVersion: {
                    amount,
                    intervalMonths: interval,
                    startDate: startDate,
                    endDate: endDate,
                    stopModificationId: null,
                },
            });
        });

        previewIncomes = detected;
    });

    function toInputMonth(isoStr: string | null): string {
        if (!isoStr) return "";
        return isoStr.substring(0, 7); // "YYYY-MM"
    }

    function fromInputMonth(val: string): string {
        if (!val) return "";
        return val + "-01T00:00:00Z";
    }

    function formatDate(dateStr: string | null) {
        if (!dateStr) return "Ongoing";
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            year: "numeric",
            month: "2-digit",
        });
    }

    onMount(() => {
        fetchData();
    });
</script>

<div class="space-y-8">
    <!-- Header -->
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div>
            <h2
                class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500"
            >
                Income Sources
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Deterministic revenue streams with automated versioning.
            </p>
        </div>
        <div class="flex gap-4">
            <button
                onclick={() => (showImportModal = true)}
                class="btn-secondary"
            >
                <ClipboardPaste class="w-4 h-4" />
                Bulk Import
            </button>
            <button
                onclick={() => {
                    currentIncome = createNewIncome();
                    amountInput = "";
                    showAddModal = true;
                }}
                class="btn-primary"
            >
                <Plus class="w-5 h-5" />
                Add Income
            </button>
        </div>
    </div>

    {#if error}
        <div
            transition:fade
            class="glass-card p-6 border-rose-200 bg-rose-50/50 flex items-center gap-4 text-rose-600"
        >
            <AlertCircle class="w-6 h-6 flex-shrink-0" />
            <div class="flex-1">
                <p class="text-xs font-black uppercase tracking-widest">
                    Node Engine Error
                </p>
                <p class="text-sm font-bold">{error}</p>
            </div>
            <button
                onclick={fetchData}
                class="px-4 py-2 bg-rose-600 text-white rounded-xl text-[10px] font-black uppercase tracking-widest hover:bg-rose-700 transition-colors shadow-lg shadow-rose-200"
            >
                Retry
            </button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <Loader2 class="w-10 h-10 text-indigo-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Syncing Versions...
            </p>
        </div>
    {:else if incomes.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner"
            >
                <Euro class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Income Active
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Start your financial model by adding a revenue source.
                </p>
            </div>
            <button
                onclick={() => (showAddModal = true)}
                class="btn-secondary mx-auto"
            >
                Create First Entry
            </button>
        </div>
    {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each incomes as income (income.id)}
                <div
                    transition:fade
                    class="glass-card p-8 group hover:border-indigo-200/50 transition-all duration-300 relative overflow-hidden"
                >
                    <div
                        class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500/0 via-indigo-500/20 to-indigo-500/0 opacity-0 group-hover:opacity-100 transition-opacity"
                    ></div>

                    <div class="flex justify-between items-start mb-8">
                        <div class="space-y-1">
                            <h3
                                class="text-xl font-black tracking-tight text-slate-900"
                            >
                                {income.name}
                            </h3>
                            <div class="flex items-center gap-2">
                                <span
                                    class="px-2 py-0.5 bg-indigo-50 text-indigo-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                >
                                    {income.activeVersion?.intervalMonths === 1
                                        ? "Monthly"
                                        : income.activeVersion
                                                .intervalMonths === 3
                                          ? "Quarterly"
                                          : "Yearly"}
                                </span>
                                <span
                                    class="px-2 py-0.5 bg-slate-100 text-slate-400 rounded-md text-[9px] font-black uppercase tracking-[0.2em] flex items-center gap-1"
                                >
                                    <History class="w-2.5 h-2.5" /> Latest
                                </span>
                                {#if income.activeVersion?.stopModificationId}
                                    {@const mod = modifications.find(
                                        (m) =>
                                            m.id ===
                                            income.activeVersion
                                                .stopModificationId,
                                    )}
                                    <span
                                        class="px-2 py-0.5 bg-amber-50 text-amber-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em] flex items-center gap-1"
                                        title="Stops when {mod?.description ||
                                            'Modification'} triggers"
                                    >
                                        <Layers class="w-2.5 h-2.5" /> Auto-Stop
                                    </span>
                                {/if}
                            </div>
                        </div>
                        <div class="flex gap-2">
                            <button
                                onclick={() => editIncome(income)}
                                class="p-2.5 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 rounded-xl transition-all border border-transparent hover:border-indigo-100"
                                title="Edit (Create New Version)"
                            >
                                <Pencil class="w-4 h-4" />
                            </button>
                            <button
                                onclick={() => {
                                    incomeToDelete = income.id!;
                                    showDeleteConfirm = true;
                                }}
                                class="p-2.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-all border border-transparent hover:border-red-100"
                                title="Delete/Revert"
                            >
                                <Trash2 class="w-4 h-4" />
                            </button>
                        </div>
                    </div>

                    <div class="space-y-6">
                        <div>
                            <p
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                            >
                                Value
                            </p>
                            <p class="text-3xl font-black text-slate-900">
                                {formatGermanAmount(
                                    income.activeVersion?.amount,
                                )} €
                            </p>
                        </div>

                        <div
                            class="flex items-center gap-6 pt-6 border-t border-slate-100"
                        >
                            <div class="space-y-1">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 flex items-center gap-1"
                                >
                                    <Calendar class="w-3 h-3" /> From
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {formatDate(
                                        income.activeVersion?.startDate,
                                    )}
                                </p>
                            </div>
                            <ArrowRight class="w-4 h-4 text-slate-200" />
                            <div class="space-y-1">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 flex items-center gap-1"
                                >
                                    <Clock class="w-3 h-3" /> To
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {formatDate(income.activeVersion?.endDate)}
                                </p>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
        </div>
    {/if}
</div>

<!-- Add/Edit Modal -->
{#if showAddModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-2xl bg-white rounded-[30px] shadow-2xl relative overflow-hidden max-h-[90vh] overflow-y-auto"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (showAddModal = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 transition-colors"
            >
                <Plus class="w-6 h-6 rotate-45" />
            </button>

            <div class="p-10 space-y-10">
                <div>
                    <h3
                        class="text-2xl font-black text-slate-900 tracking-tight"
                    >
                        {currentIncome.id ? "Refine" : "New"} Income Stream
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        {currentIncome.id
                            ? "Changes will be saved as a new immutable version."
                            : "Initialize a new deterministic revenue source."}
                    </p>
                </div>

                <form
                    onsubmit={(e) => {
                        e.preventDefault();
                        saveIncome();
                    }}
                    class="space-y-8"
                >
                    <div class="space-y-6">
                        <div class="grid grid-cols-2 gap-6">
                            <div class="space-y-2">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >Label</label
                                >
                                <input
                                    bind:value={currentIncome.name}
                                    placeholder="e.g. Primary Salary"
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
                            <SearchableMultiSelect
                                label="Planned Account Link"
                                options={virtualAccountMultiOptions}
                                bind:values={currentIncome.accountIds}
                                placeholder="Select accounts..."
                            />
                        </div>
                        <div class="grid grid-cols-2 gap-6">
                            <SearchableDropdown
                                label="Realtime Pool Link"
                                options={poolOptions}
                                bind:value={currentIncome.poolId}
                                placeholder="None / Uncategorized"
                            />
                        </div>
                    </div>

                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Amount (€)</label
                            >
                            <div class="relative">
                                <div
                                    class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none"
                                >
                                    <Euro class="w-4 h-4 text-slate-400" />
                                </div>
                                <input
                                    type="text"
                                    bind:value={amountInput}
                                    placeholder="1.234,56"
                                    class="block w-full pl-10 pr-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Interval</label
                            >
                            <select
                                bind:value={
                                    currentIncome.activeVersion.intervalMonths
                                }
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                            >
                                <option value={1}>Monthly</option>
                                <option value={3}>Quarterly</option>
                                <option value={12}>Yearly</option>
                            </select>
                        </div>
                    </div>

                    <div
                        class="p-6 bg-white rounded-2xl border border-slate-100 shadow-sm space-y-4"
                    >
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Termination Trigger</label
                            >
                            <select
                                bind:value={
                                    currentIncome.activeVersion
                                        .stopModificationId
                                }
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                            >
                                <option value={null}>None (Ongoing)</option>
                                {#each modifications.filter((m) => m.activeVersion.withdrawalPercentage > 0) as m}
                                    <option value={m.id}
                                        >{m.description} ({m.activeVersion
                                            .withdrawalPercentage}% Dynamic)</option
                                    >
                                {/each}
                            </select>
                            <p
                                class="text-[9px] font-medium text-slate-500 ml-1"
                            >
                                Automatically stop this income once the selected
                                modification triggers.
                            </p>
                        </div>
                    </div>

                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Start Month</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentIncome.activeVersion.startDate,
                                )}
                                oninput={(e: any) =>
                                    (currentIncome.activeVersion.startDate =
                                        fromInputMonth(e.target.value))}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                required
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >End Month (Optional)</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentIncome.activeVersion.endDate,
                                )}
                                oninput={(e: any) =>
                                    (currentIncome.activeVersion.endDate = e
                                        .target.value
                                        ? fromInputMonth(e.target.value)
                                        : null)}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                            />
                        </div>
                    </div>

                    <!-- Scenario Linker (New Entities Only) -->
                    {#if !currentIncome.id && scenarios.length > 0}
                        <div class="space-y-4 pt-4 border-t border-slate-100">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                >Contextual Onboarding</label
                            >
                            <p class="text-[10px] text-slate-500 font-medium">
                                Auto-link this income to specific financial
                                scenarios:
                            </p>
                            <div class="flex flex-wrap gap-2">
                                {#each scenarios as s}
                                    <button
                                        type="button"
                                        onclick={() => {
                                            if (
                                                currentIncome.linkToScenarios?.includes(
                                                    s.id,
                                                )
                                            ) {
                                                currentIncome.linkToScenarios =
                                                    currentIncome.linkToScenarios.filter(
                                                        (id) => id !== s.id,
                                                    );
                                            } else {
                                                currentIncome.linkToScenarios =
                                                    [
                                                        ...(currentIncome.linkToScenarios ||
                                                            []),
                                                        s.id,
                                                    ];
                                            }
                                        }}
                                        class="px-3 py-1.5 border rounded-xl text-[10px] font-black transition-all flex items-center gap-2
                                           {currentIncome.linkToScenarios?.includes(
                                            s.id,
                                        )
                                            ? 'bg-indigo-600 border-indigo-600 text-white shadow-lg shadow-indigo-100'
                                            : 'bg-white border-slate-200 text-slate-500 hover:border-slate-300'}"
                                    >
                                        <Layers class="w-3 h-3" />
                                        {s.name}
                                    </button>
                                {/each}
                            </div>
                        </div>
                    {/if}

                    <div class="pt-6">
                        <button
                            disabled={isSaving}
                            class="btn-primary w-full py-4 text-lg shadow-2xl shadow-indigo-100 bg-indigo-600 hover:bg-indigo-700"
                        >
                            {#if isSaving}
                                <Loader2 class="w-6 h-6 animate-spin" />
                                <span>Versioning Data...</span>
                            {:else}
                                <span>Save as New Version</span>
                            {/if}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    </div>
{/if}

{#if showImportModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-5xl bg-white rounded-[30px] shadow-2xl relative max-h-[90vh] flex flex-col overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (showImportModal = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 transition-colors"
            >
                <Plus class="w-6 h-6 rotate-45" />
            </button>

            <div class="p-10 flex flex-col overflow-hidden h-full">
                <div class="mb-8">
                    <h3
                        class="text-3xl font-black text-slate-900 tracking-tight"
                    >
                        Income Verification Engine
                    </h3>
                    <p class="text-slate-500 font-medium">
                        Deterministic review of external spreadsheet data.
                    </p>
                </div>

                <div
                    class="grid grid-cols-1 lg:grid-cols-12 gap-10 flex-1 overflow-hidden"
                >
                    <div
                        class="lg:col-span-4 flex flex-col space-y-6 overflow-hidden"
                    >
                        <div class="flex-1 flex flex-col space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                >TSV Source Data</label
                            >
                            <textarea
                                bind:value={rawImportData}
                                placeholder="Paste columns from Sheets..."
                                class="flex-1 w-full p-4 bg-white border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 outline-none font-mono text-[10px] leading-relaxed resize-none shadow-sm"
                            ></textarea>
                        </div>

                        <div
                            class="p-5 bg-white rounded-2xl border border-slate-100 shadow-sm space-y-4"
                        >
                            <p
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400"
                            >
                                Numeric Parsing Strategy
                            </p>
                            <div
                                class="flex p-1 bg-white border border-slate-200 rounded-xl"
                            >
                                <button
                                    onclick={() => (importLocale = "US")}
                                    class="flex-1 py-2 px-3 rounded-lg text-[11px] font-black flex items-center justify-center gap-2 transition-all
                                           {importLocale === 'US'
                                        ? 'bg-indigo-600 text-white shadow-lg'
                                        : 'text-slate-400 hover:text-slate-600'}"
                                >
                                    <Globe class="w-3.5 h-3.5" /> International (.)
                                </button>
                                <button
                                    onclick={() => (importLocale = "DE")}
                                    class="flex-1 py-2 px-3 rounded-lg text-[11px] font-black flex items-center justify-center gap-2 transition-all
                                           {importLocale === 'DE'
                                        ? 'bg-indigo-600 text-white shadow-lg'
                                        : 'text-slate-400 hover:text-slate-600'}"
                                >
                                    <Languages class="w-3.5 h-3.5" /> German (,)
                                </button>
                            </div>
                        </div>
                    </div>

                    <div class="lg:col-span-8 flex flex-col overflow-hidden">
                        <div class="flex items-center justify-between mb-4">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                >Verification List ({previewIncomes.length} detected)</label
                            >
                            <div class="flex gap-2">
                                <button
                                    onclick={() =>
                                        previewIncomes.forEach(
                                            (i) => (i.import_selected = true),
                                        )}
                                    class="text-[9px] font-black text-indigo-600 hover:underline uppercase"
                                    >Select All</button
                                >
                                <span class="text-slate-200">|</span>
                                <button
                                    onclick={() =>
                                        previewIncomes.forEach(
                                            (i) => (i.import_selected = false),
                                        )}
                                    class="text-[9px] font-black text-slate-400 hover:underline uppercase"
                                    >Clear All</button
                                >
                            </div>
                        </div>

                        <div
                            class="flex-1 overflow-y-auto border border-slate-100 rounded-2xl bg-white shadow-sm"
                        >
                            <table class="w-full text-left border-collapse">
                                <thead class="sticky top-0 bg-white shadow-sm">
                                    <tr>
                                        <th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400 w-12"
                                        ></th>
                                        <th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400"
                                            >Identity</th
                                        >
                                        <th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400"
                                            >Deterministic Value</th
                                        >
                                        <th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400"
                                            >Timeline</th
                                        >
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each previewIncomes as inc, i}
                                        <tr
                                            class="group hover:bg-slate-50 transition-colors border-b border-slate-50 last:border-0"
                                        >
                                            <td class="p-4 text-center">
                                                <button
                                                    onclick={() =>
                                                        (inc.import_selected =
                                                            !inc.import_selected)}
                                                    class="w-6 h-6 rounded-md border-2 transition-all flex items-center justify-center
                                                           {inc.import_selected
                                                        ? 'bg-indigo-600 border-indigo-600 text-white'
                                                        : 'border-slate-200 bg-white text-transparent'}"
                                                >
                                                    <Check class="w-4 h-4" />
                                                </button>
                                            </td>
                                            <td class="p-4">
                                                <p
                                                    class="font-black text-slate-900 text-sm truncate max-w-[150px]"
                                                >
                                                    {inc.name}
                                                </p>
                                            </td>
                                            <td class="p-4">
                                                <p
                                                    class="font-black text-slate-900 text-sm {inc
                                                        .activeVersion
                                                        .amount === 0
                                                        ? 'text-rose-500'
                                                        : ''}"
                                                >
                                                    {formatGermanAmount(
                                                        inc.activeVersion
                                                            .amount,
                                                    )} €
                                                </p>
                                                <p
                                                    class="text-[9px] font-bold text-slate-400 uppercase tracking-tighter"
                                                >
                                                    Interval: {inc.activeVersion
                                                        .intervalMonths}m
                                                </p>
                                            </td>
                                            <td class="p-4">
                                                <div
                                                    class="flex items-center gap-2 text-[10px] font-bold text-slate-500 uppercase"
                                                >
                                                    <span
                                                        >{formatDate(
                                                            inc.activeVersion
                                                                .startDate,
                                                        )}</span
                                                    >
                                                    <ArrowRight
                                                        class="w-3 h-3 text-slate-300"
                                                    />
                                                    <span
                                                        >{formatDate(
                                                            inc.activeVersion
                                                                .endDate,
                                                        )}</span
                                                    >
                                                </div>
                                            </td>
                                        </tr>
                                    {/each}
                                    {#if previewIncomes.length === 0}
                                        <tr>
                                            <td
                                                colspan="4"
                                                class="p-12 text-center"
                                            >
                                                <div
                                                    class="flex flex-col items-center gap-2 opacity-30"
                                                >
                                                    <Wallet class="w-10 h-10" />
                                                    <p
                                                        class="text-xs font-black uppercase tracking-[0.2em]"
                                                    >
                                                        Awaiting Data Node...
                                                    </p>
                                                </div>
                                            </td>
                                        </tr>
                                    {/if}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </div>

                <div
                    class="mt-10 pt-8 border-t border-slate-100 flex items-center justify-between"
                >
                    <div
                        class="flex items-center gap-3 text-indigo-600 bg-indigo-50 px-4 py-2 rounded-xl border border-indigo-100"
                    >
                        <CheckCircle2 class="w-4 h-4" />
                        <p
                            class="text-[10px] font-black uppercase tracking-[0.2em]"
                        >
                            Verified: {previewIncomes.filter(
                                (i) => i.import_selected,
                            ).length} units
                        </p>
                    </div>

                    <div class="flex gap-4">
                        <button
                            onclick={() => {
                                rawImportData = "";
                                showImportModal = false;
                            }}
                            class="btn-secondary px-8"
                        >
                            Abort
                        </button>
                        <button
                            onclick={executeImport}
                            disabled={!previewIncomes.some(
                                (i) => i.import_selected,
                            ) || isSaving}
                            class="btn-primary px-12 py-4 text-lg shadow-2xl shadow-indigo-100"
                        >
                            {#if isSaving}
                                <Loader2 class="w-6 h-6 animate-spin" />
                                <span>Persisting Batch...</span>
                            {:else}
                                <span>Confirm & Commit</span>
                            {/if}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    </div>
{/if}

<!-- Deletion Confirmation Modal -->
{#if showDeleteConfirm}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-md bg-white rounded-[30px] shadow-2xl p-10 relative overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="text-center space-y-2 mb-8">
                <h3 class="text-2xl font-black text-slate-900">
                    Manage Lifecycle
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    How should the WealthEngine handle this deletion?
                </p>
            </div>

            <div class="grid grid-cols-1 gap-4">
                <button
                    onclick={() => confirmDelete("revert")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-indigo-100 hover:bg-indigo-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-indigo-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Undo2 class="w-6 h-6 text-indigo-600" />
                    </div>
                    <div>
                        <p class="font-black text-slate-900 leading-tight">
                            Revert to Previous
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Delete only the latest version record.
                        </p>
                    </div>
                </button>

                <button
                    onclick={() => confirmDelete("full")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-rose-100 hover:bg-rose-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-rose-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Archive class="w-6 h-6 text-rose-600" />
                    </div>
                    <div>
                        <p class="font-black text-slate-900 leading-tight">
                            Full Archive
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Hide this income source and all versions.
                        </p>
                    </div>
                </button>
            </div>

            <button
                onclick={() => {
                    showDeleteConfirm = false;
                    incomeToDelete = null;
                }}
                class="w-full mt-8 py-3 text-slate-400 font-black uppercase tracking-[0.2em] text-[10px] hover:text-slate-900 transition-colors"
            >
                Cancel Action
            </button>
        </div>
    </div>
{/if}
