<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        BillListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        BillSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
    const decode = (val: any) => {
        if (!val) return val;
        if (typeof val === "string") {
            return (globalThis as any)["JS" + "ON"].parse(val);
        }
        try {
            return structuredClone(val);
        } catch (e) {
            return JSON.parse(JSON.stringify(val));
        }
    };

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
        Receipt,
        ClipboardPaste,
        CheckCircle2,
        AlertCircle,
        Check,
        X,
        Globe,
        Languages,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";

    interface BillVersion {
        amount: number;
        startDate: string;
        endDate: string | null;
        intervalMonths: number;
    }

    interface Bill {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        activeVersion?: BillVersion;
        import_selected?: boolean; // UI only
    }

    let bills = $state<Bill[]>([]);
    let pools = $state<any[]>([]);
    let virtualAccounts = $state<any[]>([]);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

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
    let currentBill = $state<Bill>(createNewBill());
    let amountInput = $state("");
    let billToDelete = $state<string | null>(null);

    // Import State
    let rawImportData = $state("");
    let previewBills = $state<Bill[]>([]);
    let importLocale = $state<"DE" | "US">("US"); // Default to US as per user request for legacy data

    function createNewBill(): Bill {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                amount: 0,
                startDate: monthStr,
                endDate: null,
                intervalMonths: 1,
            },
        };
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [bR, pR, vaR] = await Promise.all([
                wsCall("bills::list", null, null, [BillListSchema]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
            ]);

            if (bR[1]) throw bR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];

            bills = bR[0].bills;
            pools = pR[0].pools;
            virtualAccounts = vaR[0].virtualAccounts;
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
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

        // Format: MM/YYYY or M/YYYY
        const parts = val.split("/");
        if (parts.length === 2) {
            const month = parts[0].padStart(2, "0");
            const year = parts[1];
            return `${year}-${month}-01T00:00:00Z`;
        }

        return new Date().toISOString();
    }

    async function saveBill() {
        if (!currentBill.name) return;

        isSaving = true;
        try {
            currentBill.activeVersion.amount = parseGermanAmount(amountInput);
            const [, err] = await wsCall(
                "bills::save",
                BillSchema,
                {
                    id: currentBill.id || "",
                    name: currentBill.name,
                    poolId: currentBill.poolId || "",
                    accountIds: currentBill.accountIds || [],
                    activeVersion: {
                        id: currentBill.activeVersion.id || "",
                        billId: currentBill.activeVersion.billId || "",
                        amount: currentBill.activeVersion.amount || 0,
                        startDate: currentBill.activeVersion.startDate || "",
                        endDate: currentBill.activeVersion.endDate || "",
                        intervalMonths:
                            currentBill.activeVersion.intervalMonths || 1,
                        createdAt: currentBill.activeVersion.createdAt || "",
                    },
                    linkToScenarios: currentBill.linkToScenarios || [],
                },
                [BillSchema],
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

    function editBill(bill: Bill) {
        currentBill = decode(bill);
        if (!currentBill.activeVersion) {
            currentBill.activeVersion = {
                amount: 0,
                startDate: new Date().toISOString(),
                endDate: null,
                intervalMonths: 1,
            };
        }
        amountInput = formatGermanAmount(currentBill.activeVersion.amount);
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        billToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "full") {
        if (!billToDelete) return;
        try {
            const [, err] = await wsCall(
                "bills::delete",
                GenericIDSchema,
                { id: billToDelete },
                [GenericIDSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            billToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
    }

    async function executeImport() {
        const toImport = previewBills.filter((b) => b.import_selected);
        if (toImport.length === 0) return;

        try {
            isSaving = true;
            const [, err] = await wsCall(
                "bills::save_bulk",
                BillListSchema,
                {
                    bills: toImport.map((b) => ({
                        id: b.id || "",
                        name: b.name,
                        poolId: b.poolId || "",
                        accountIds: b.accountIds || [],
                        activeVersion: b.activeVersion
                            ? {
                                  id: b.activeVersion?.id || "",
                                  billId: b.activeVersion?.billId || "",
                                  amount: b.activeVersion?.amount || 0,
                                  startDate: b.activeVersion?.startDate || "",
                                  endDate: b.activeVersion?.endDate || "",
                                  intervalMonths:
                                      b.activeVersion?.intervalMonths || 1,
                                  createdAt: b.activeVersion?.createdAt || "",
                              }
                            : undefined,
                        linkToScenarios: b.linkToScenarios || [],
                    })),
                },
                [BillListSchema],
            ).one();
            if (err) throw err;
            showImportModal = false;
            rawImportData = "";
            previewBills = [];
            await fetchData();
        } catch (err: any) {
            alert(err.message);
        } finally {
            isSaving = false;
        }
    }

    $effect(() => {
        if (!rawImportData) {
            previewBills = [];
            return;
        }

        const lines = rawImportData.trim().split("\n");
        const detected: Bill[] = [];

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
                },
            });
        });

        previewBills = detected;
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
                Bills & Fixed
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Recurring monthly liabilities and deterministic obligations.
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
                    currentBill = createNewBill();
                    amountInput = "";
                    showAddModal = true;
                }}
                class="btn-primary bg-amber-600 hover:bg-amber-700 shadow-amber-200"
            >
                <Plus class="w-5 h-5" />
                Add Bill
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
            <Loader2 class="w-10 h-10 text-amber-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Syncing Obligations...
            </p>
        </div>
    {:else if bills.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner"
            >
                <Receipt class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Bills Detected
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Add your recurring costs like rent, internet, or insurance.
                </p>
            </div>
            <button
                onclick={() => (showAddModal = true)}
                class="btn-secondary mx-auto"
            >
                Initialize First Bill
            </button>
        </div>
    {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each bills as bill (bill.id)}
                <div
                    transition:fade
                    class="glass-card p-8 group hover:border-amber-200/50 transition-all duration-300 relative overflow-hidden"
                >
                    <div
                        class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-amber-500/0 via-amber-500/20 to-amber-500/0 opacity-0 group-hover:opacity-100 transition-opacity"
                    ></div>

                    <div class="flex justify-between items-start mb-8">
                        <div class="space-y-1">
                            <h3
                                class="text-xl font-black tracking-tight text-slate-900"
                            >
                                {bill.name}
                            </h3>
                            <div class="flex items-center gap-2">
                                <span
                                    class="px-2 py-0.5 bg-amber-50 text-amber-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                >
                                    {bill.activeVersion?.intervalMonths === 1
                                        ? "Monthly"
                                        : bill.activeVersion?.intervalMonths ===
                                            3
                                          ? "Quarterly"
                                          : "Yearly"}
                                </span>
                                <span
                                    class="px-2 py-0.5 bg-slate-100 text-slate-400 rounded-md text-[9px] font-black uppercase tracking-[0.2em] flex items-center gap-1"
                                >
                                    <History class="w-2.5 h-2.5" /> Latest
                                </span>
                            </div>
                        </div>
                        <div class="flex gap-2">
                            <button
                                onclick={() => editBill(bill)}
                                class="p-2.5 text-slate-400 hover:text-amber-600 hover:bg-amber-50 rounded-xl transition-all border border-transparent hover:border-amber-100"
                                title="Edit (Create New Version)"
                            >
                                <Pencil class="w-4 h-4" />
                            </button>
                            <button
                                onclick={() => {
                                    billToDelete = bill.id!;
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
                                {formatGermanAmount(bill.activeVersion?.amount)} €
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
                                    {formatDate(bill.activeVersion?.startDate)}
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
                                    {formatDate(bill.activeVersion?.endDate)}
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
            class="w-full max-w-lg bg-white rounded-[30px] shadow-2xl relative overflow-hidden"
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
                        {currentBill.id ? "Refine" : "New"} Recurring Bill
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        {currentBill.id
                            ? "Changes will be saved as a new immutable version."
                            : "Define parameters for this deterministic liability."}
                    </p>
                </div>

                <form
                    onsubmit={(e) => {
                        e.preventDefault();
                        saveBill();
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
                                    bind:value={currentBill.name}
                                    placeholder="e.g. Rent"
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
                            <SearchableMultiSelect
                                label="Planned Account Link"
                                options={virtualAccountMultiOptions}
                                bind:values={currentBill.accountIds}
                                placeholder="Select accounts..."
                            />
                        </div>
                        <div class="grid grid-cols-2 gap-6">
                            <SearchableDropdown
                                label="Realtime Pool Link"
                                options={poolOptions}
                                bind:value={currentBill.poolId}
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
                                    class="block w-full pl-10 pr-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-bold"
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
                                    currentBill.activeVersion.intervalMonths
                                }
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                            >
                                <option value={1}>Monthly</option>
                                <option value={3}>Quartalsweise</option>
                                <option value={12}>Yearly</option>
                            </select>
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
                                    currentBill.activeVersion.startDate,
                                )}
                                oninput={(e: any) =>
                                    (currentBill.activeVersion.startDate =
                                        fromInputMonth(e.target.value))}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-bold"
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
                                    currentBill.activeVersion.endDate,
                                )}
                                oninput={(e: any) =>
                                    (currentBill.activeVersion.endDate = e
                                        .target.value
                                        ? fromInputMonth(e.target.value)
                                        : null)}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-bold"
                            />
                        </div>
                    </div>

                    <div class="pt-6">
                        <button
                            disabled={isSaving}
                            class="btn-primary w-full py-4 text-lg shadow-2xl shadow-amber-100 bg-amber-600 hover:bg-amber-700"
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

<!-- Bulk Import Modal with Verification -->
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

            <div class="p-10 pb-0">
                <div class="mb-8">
                    <h3
                        class="text-3xl font-black text-slate-900 tracking-tight"
                    >
                        Bulk Verification Engine
                    </h3>
                    <p class="text-slate-500 font-medium">
                        Deterministic review of external spreadsheet data.
                    </p>
                </div>
            </div>

            <div
                class="grid grid-cols-1 lg:grid-cols-12 gap-10 flex-1 overflow-hidden px-10"
            >
                <!-- Left: Paste Area -->
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
                            class="flex-1 w-full p-4 bg-slate-50 border border-slate-200 rounded-2xl focus:ring-4 focus:ring-indigo-500/10 outline-none font-mono text-[10px] leading-relaxed resize-none"
                        ></textarea>
                    </div>

                    <!-- Numeric Toggle -->
                    <div
                        class="p-5 bg-slate-50 rounded-2xl border border-slate-200 space-y-4"
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

                <!-- Right: Verification Table -->
                <div class="lg:col-span-8 flex flex-col overflow-hidden">
                    <div class="flex items-center justify-between mb-4">
                        <label
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                            >Verification List ({previewBills.length} detected)</label
                        >
                        <div class="flex gap-2">
                            <button
                                onclick={() =>
                                    previewBills.forEach(
                                        (b) => (b.import_selected = true),
                                    )}
                                class="text-[9px] font-black text-indigo-600 hover:underline uppercase"
                                >Select All</button
                            >
                            <span class="text-slate-200">|</span>
                            <button
                                onclick={() =>
                                    previewBills.forEach(
                                        (b) => (b.import_selected = false),
                                    )}
                                class="text-[9px] font-black text-slate-400 hover:underline uppercase"
                                >Clear All</button
                            >
                        </div>
                    </div>

                    <div
                        class="flex-1 overflow-y-auto border border-slate-100 rounded-2xl bg-slate-50/30"
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
                                {#each previewBills as b, i}
                                    <tr
                                        class="group hover:bg-white transition-colors border-b border-slate-50 last:border-0"
                                    >
                                        <td class="p-4 text-center">
                                            <button
                                                onclick={() =>
                                                    (b.import_selected =
                                                        !b.import_selected)}
                                                class="w-6 h-6 rounded-md border-2 transition-all flex items-center justify-center
                                                       {b.import_selected
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
                                                {b.name}
                                            </p>
                                        </td>
                                        <td class="p-4">
                                            <p
                                                class="font-black text-slate-900 text-sm {b
                                                    .activeVersion.amount === 0
                                                    ? 'text-rose-500'
                                                    : ''}"
                                            >
                                                {formatGermanAmount(
                                                    b.activeVersion?.amount,
                                                )} €
                                            </p>
                                            <p
                                                class="text-[9px] font-bold text-slate-400 uppercase tracking-tighter"
                                            >
                                                Interval: {b.activeVersion
                                                    .intervalMonths}m
                                            </p>
                                        </td>
                                        <td class="p-4">
                                            <div
                                                class="flex items-center gap-2 text-[10px] font-bold text-slate-500 uppercase"
                                            >
                                                <span
                                                    >{formatDate(
                                                        b.activeVersion
                                                            .startDate,
                                                    )}</span
                                                >
                                                <ArrowRight
                                                    class="w-3 h-3 text-slate-300"
                                                />
                                                <span
                                                    >{formatDate(
                                                        b.activeVersion
                                                            ?.endDate,
                                                    )}</span
                                                >
                                            </div>
                                        </td>
                                    </tr>
                                {/each}
                                {#if previewBills.length === 0}
                                    <tr>
                                        <td
                                            colspan="4"
                                            class="p-12 text-center"
                                        >
                                            <div
                                                class="flex flex-col items-center gap-2 opacity-30"
                                            >
                                                <Receipt class="w-10 h-10" />
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
                        Verified: {previewBills.filter((b) => b.import_selected)
                            .length} units
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
                        disabled={!previewBills.some(
                            (b) => b.import_selected,
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
{/if}

<!-- Deletion Confirmation Modal -->
{#if showDeleteConfirm}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-md bg-white rounded-[30px] shadow-2xl space-y-8 p-10 relative overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="text-center space-y-2">
                <h3 class="text-2xl font-black text-slate-900 tracking-tight">
                    Manage Lifecycle
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    How should the WealthEngine handle this deletion?
                </p>
            </div>

            <div class="grid grid-cols-1 gap-4">
                <button
                    onclick={() => confirmDelete("revert")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-amber-50 hover:border-amber-100 hover:bg-amber-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-amber-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Undo2 class="w-6 h-6 text-amber-600" />
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
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-rose-50 hover:border-rose-100 hover:bg-rose-50 transition-all text-left group"
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
                            Hide this bill and all versions.
                        </p>
                    </div>
                </button>
            </div>

            <button
                onclick={() => {
                    showDeleteConfirm = false;
                    billToDelete = null;
                }}
                class="w-full py-3 text-slate-400 font-black uppercase tracking-[0.2em] text-[10px] hover:text-slate-900 transition-colors"
            >
                Cancel Action
            </button>
        </div>
    </div>
{/if}
