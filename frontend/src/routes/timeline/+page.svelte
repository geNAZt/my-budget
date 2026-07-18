<script lang="ts">
    import { onMount } from "svelte";
    import { fade, slide } from "svelte/transition";
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        ScenarioListSchema,
        ScenarioSchema,
        GenericIDSchema,
        ErrorSchema,
        AssetListSchema,
        LoanListSchema,
        IncomeListSchema,
        BillListSchema,
        ExpenseListSchema,
        VirtualAccountListSchema,
        ProjectionMonthSchema,
        YieldMapSchema,
        PerformanceMetricsSchema,
        ExpenseSchema,
        AssetSchema,
    } from "$lib/gen/api_pb.js";
    import {
        Activity,
        ArrowRight,
        Calendar,
        ChevronRight,
        ChevronDown,
        CreditCard,
        Cpu,
        Euro,
        Layers,
        Loader2,
        Plus,
        RefreshCw,
        Settings,
        Trash2,
        TrendingUp,
        User,
        Zap,
        Check,
        CheckCircle2,
        X,
        GripVertical,
        TrendingDown,
        Lightbulb,
        Info,
        ArrowDown,
        ArrowUp,
        Lock,
        Unlock,
        Link2,
        DollarSign,
        Edit3,
        Sparkles
    } from "@lucide/svelte";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
    import { toInputMonth, fromInputMonth } from "$lib/utils/date";
    import ExpenseDetailModal from "./components/ExpenseDetailModal.svelte";
    import AssetDetailModal from "./components/AssetDetailModal.svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    interface TimeSlice {
        id?: string;
        amount: number;
        intervalMonths: number;
        startDate: string;
        endDate: string | null;
        description: string;
    }

    interface SubExpense {
        id?: string;
        description: string;
        amount: number;
        metadata?: Record<string, string>;
        metadataList?: { key: string; value: string }[];
    }

    interface ExpenseVersion {
        id?: string;
        expenseId?: string;
        amount: number;
        dueDate: string;
        createdAt?: string;
        slices: TimeSlice[];
        subExpenses?: SubExpense[];
    }

    interface Expense {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        linkToScenarios?: string[];
        activeVersion?: ExpenseVersion;
    }

    interface Scenario {
        id?: string;
        name: string;
        description: string;
        projectionMonths: number;
        remainderOrder: string[];
        isActive: boolean;
        monthStartDay: number;
        startDate: string;
        entities: {
            entityId: string;
            entityType: string;
            versionId: string;
        }[];
        simulations: number;
        simYears: number;
        simPercent: number;
        lookbackYears: number;
        mcImplementation: string;
        passiveIncomePercentage: number;
        etfParams?: Record<string, any>;
    }

    // --- State Runes ---
    let scenarios = $state<Scenario[]>([]);
    let selectedScenarioId = $state<string | null>(null);
    let activeScenario = $state<Scenario | null>(null);
    
    let projectionMonths = $state<number>(24);
    let months = $state<any[]>([]);
    
    let allAssets = $state<any[]>([]);
    let allLoans = $state<any[]>([]);
    let allIncomes = $state<any[]>([]);
    let allBills = $state<any[]>([]);
    let allExpenses = $state<any[]>([]);
    let allVirtualAccounts = $state<any[]>([]);

    let isLoading = $state(true);
    let isProjecting = $state(false);
    let error = $state<string | null>(null);

    // Drag and Drop States
    let draggedWaterfallIndex = $state<number | null>(null);
    let dragOverWaterfallIndex = $state<number | null>(null);
    let activeDragOverMonthStr = $state<string | null>(null);

    // Drag to scroll states
    let timelineScrollContainer = $state<HTMLElement | null>(null);
    let isDraggingTimeline = $state(false);
    let startX = $state(0);
    let scrollLeft = $state(0);

    // Sub-asset resizing states
    let isResizingSubAsset = $state(false);
    let resizingSubAssetData = $state<{
        asset: any;
        sa: any;
        side: 'start' | 'end';
        originalStartIdx: number;
        originalEndIdx: number;
        currentStartIdx: number;
        currentEndIdx: number;
    } | null>(null);

    // Hover highlight states for link tracing
    let hoveredEntityId = $state<string | null>(null);
    let hoveredEntityType = $state<string | null>(null);

    // Detail Modal for clicked Expense
    let showExpenseModal = $state(false);
    let selectedExpense = $state<any | null>(null);
    let selectedExpenseObj = $state<any | null>(null);
    let isFlexExpenseState = $state(false);
    let modalMode = $state<"edit" | "funding">("edit");
    
    // Funding plan configurations
    let fundingAssetId = $state<string>("");
    let fundingSubAssetCreated = $state(false);
    let fundingMessage = $state<string | null>(null);
    let fundingRemainderConsumer = $state(false);


    // Detail Modal for clicked Asset
    let showAssetModal = $state(false);
    let editingAsset = $state<any | null>(null);
    let editingAssetObj = $state<any | null>(null);
    let assetSaveError = $state<string | null>(null);
    let simulatedYields = $state<Record<string, number>>({});

    // Collapsible sections
    let assetsCollapsed = $state(false);
    let loansCollapsed = $state(false);
    let collapsedAssetIds = $state<string[]>([]);

    function toggleAssetSubAssets(assetId: string, event: MouseEvent) {
        event.stopPropagation();
        if (collapsedAssetIds.includes(assetId)) {
            collapsedAssetIds = collapsedAssetIds.filter(id => id !== assetId);
        } else {
            collapsedAssetIds = [...collapsedAssetIds, assetId];
        }
    }

    function handleTimelineMouseDown(e: MouseEvent) {
        if (!timelineScrollContainer) return;
        isDraggingTimeline = true;
        startX = e.pageX - timelineScrollContainer.offsetLeft;
        scrollLeft = timelineScrollContainer.scrollLeft;
    }

    function handleTimelineMouseLeave() {
        isDraggingTimeline = false;
    }

    function handleTimelineMouseUp() {
        isDraggingTimeline = false;
    }

    function handleTimelineMouseMove(e: MouseEvent) {
        if (!isDraggingTimeline || !timelineScrollContainer) return;
        e.preventDefault();
        const x = e.pageX - timelineScrollContainer.offsetLeft;
        const walk = (x - startX) * 1.5; // multiplier for faster scroll
        timelineScrollContainer.scrollLeft = scrollLeft - walk;
    }

    // Auto Optimization recommendations
    let isOptimizing = $state(false);
    let optimizationResults = $state<any[] | null>(null);
    let expenseOptimizationResults = $state<any[] | null>(null);

    // --- Helper Utilities ---
    function getID(entity: any): string {
        return entity?.id || entity?.Id || entity?.ID || "";
    }

    function getName(entity: any): string {
        return entity?.name || entity?.Name || "";
    }

    function getActiveVersion(entity: any): any {
        return entity?.activeVersion || entity?.active_version || entity?.ActiveVersion;
    }

    function getSubAssets(entity: any) {
        const v = getActiveVersion(entity);
        return v?.subAssets || v?.sub_assets || v?.SubAssets || [];
    }

    function getInterestRate(entity: any): number {
        const id = getID(entity);
        const isAsset = allAssets.some(a => getID(a) === id);
        if (isAsset && id && simulatedYields[id] !== undefined) {
            return simulatedYields[id];
        }
        const v = getActiveVersion(entity);
        return v?.interestRate || v?.interest_rate || 0;
    }

    function getBalance(entity: any): number {
        const v = getActiveVersion(entity);
        if (entity?.amountLent !== undefined || v?.amountLent !== undefined || v?.amount_lent !== undefined) {
            // It's a loan
            return v?.amountLent || v?.amount_lent || 0;
        }
        // It's an asset: sum starting balance of linked virtual accounts
        const accountIds = entity?.accountIds || entity?.account_ids || [];
        let balance = 0;
        for (const vaId of accountIds) {
            const va = allVirtualAccounts.find(v => getID(v) === vaId);
            if (va) {
                const vVersion = getActiveVersion(va);
                balance += vVersion?.startingBalance || vVersion?.starting_balance || 0;
            }
        }
        return balance;
    }

    function isEntityActive(entityId: string, entityType: string): boolean {
        if (!activeScenario) return false;
        if (!activeScenario.entities || activeScenario.entities.length === 0) return true;
        return activeScenario.entities.some(
            (e) => e.entityId === entityId && e.entityType === entityType
        );
    }

    function areEntitiesLinked(id1: string, type1: string, id2: string, type2: string): boolean {
        if (id1 === id2 && type1 === type2) return true;
        
        // Asset to Loan
        if (type1 === "ASSET" && type2 === "LOAN") {
            const asset = allAssets.find(a => getID(a) === id1);
            if (!asset) return false;
            const av = getActiveVersion(asset);
            if (av?.dumpingLoanId === id2) return true;
            const subAssets = av?.subAssets || [];
            return subAssets.some((sa: any) => sa.dumpingLoanId === id2);
        }
        if (type1 === "LOAN" && type2 === "ASSET") {
            return areEntitiesLinked(id2, "ASSET", id1, "LOAN");
        }
        
        // Sub-Asset to Loan
        if (type1 === "SUB_ASSET" && type2 === "LOAN") {
            for (const asset of allAssets) {
                const av = getActiveVersion(asset);
                const sa = av?.subAssets?.find((s: any) => s.id === id1);
                if (sa && sa.dumpingLoanId === id2) return true;
            }
            return false;
        }
        if (type1 === "LOAN" && type2 === "SUB_ASSET") {
            return areEntitiesLinked(id2, "SUB_ASSET", id1, "LOAN");
        }
        
        // Asset to Expense
        if (type1 === "ASSET" && type2 === "EXPENSE") {
            const asset = allAssets.find(a => getID(a) === id1);
            const expense = allExpenses.find(e => getID(e) === id2);
            if (!asset || !expense) return false;
            const av = getActiveVersion(asset);
            const subAssets = av?.subAssets || [];
            return subAssets.some((sa: any) => sa.expenseId === expense.id);
        }
        if (type1 === "EXPENSE" && type2 === "ASSET") {
            return areEntitiesLinked(id2, "ASSET", id1, "EXPENSE");
        }
        
        // Sub-Asset to Expense
        if (type1 === "SUB_ASSET" && type2 === "EXPENSE") {
            const expense = allExpenses.find(e => getID(e) === id2);
            if (!expense) return false;
            for (const asset of allAssets) {
                const av = getActiveVersion(asset);
                const sa = av?.subAssets?.find((s: any) => s.id === id1);
                if (sa && sa.expenseId === expense.id) return true;
            }
            return false;
        }
        if (type1 === "EXPENSE" && type2 === "SUB_ASSET") {
            return areEntitiesLinked(id2, "SUB_ASSET", id1, "EXPENSE");
        }

        // Asset to Sub-Asset
        if (type1 === "ASSET" && type2 === "SUB_ASSET") {
            const asset = allAssets.find(a => getID(a) === id1);
            if (!asset) return false;
            const av = getActiveVersion(asset);
            const subAssets = av?.subAssets || [];
            return subAssets.some((sa: any) => sa.id === id2);
        }
        if (type1 === "SUB_ASSET" && type2 === "ASSET") {
            return areEntitiesLinked(id2, "ASSET", id1, "SUB_ASSET");
        }
        
        return false;
    }

    function getHighlightClasses(entityId: string, entityType: string): string {
        if (!hoveredEntityId) return "transition-all duration-300";
        const linked = areEntitiesLinked(hoveredEntityId, hoveredEntityType!, entityId, entityType);
        if (linked) {
            return "ring-4 ring-indigo-500/80 ring-offset-2 dark:ring-offset-slate-900 scale-[1.01] shadow-lg transition-all duration-300 z-30";
        } else {
            return "opacity-25 scale-[0.98] transition-all duration-300";
        }
    }

    // Fixed vs Flexible Checks
    function isFlexibleExpense(name: string): boolean {
        return name.endsWith(" (Flexible)") || name.includes("[Flex]");
    }

    function cleanExpenseName(name: string): string {
        return name.replace(" (Flexible)", "").replace("[Flex]", "").trim();
    }

    // Date Utilities
    function getMonthsBetween(d1Str: string, d2Str: string): number {
        const d1 = new Date(d1Str);
        const d2 = new Date(d2Str);
        return (d2.getFullYear() - d1.getFullYear()) * 12 + (d2.getMonth() - d1.getMonth());
    }



    // Derived filtered arrays
    const activeAssets = $derived(allAssets.filter(a => isEntityActive(getID(a), "ASSET")));
    const activeLoans = $derived(allLoans.filter(l => isEntityActive(getID(l), "LOAN")));
    const activeExpenses = $derived(allExpenses.filter(e => isEntityActive(getID(e), "EXPENSE")));
    const activeBills = $derived(allBills.filter(b => isEntityActive(getID(b), "BILL")));

    const inactiveWaterfallItems = $derived([
        ...activeAssets.filter(a => !activeScenario?.remainderOrder.includes(getID(a))),
        ...activeLoans.filter(l => !activeScenario?.remainderOrder.includes(getID(l)))
    ]);

    // Format utility
    function formatCurrency(val: number) {
        if (val === undefined || val === null) return "0,00";
        return val.toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        }) + " €";
    }

    function parseMonthYear(dateStr: string) {
        if (!dateStr) return "";
        const d = new Date(dateStr);
        return d.toLocaleDateString("en-US", { year: "numeric", month: "short" });
    }

    function getMonthKey(dateStr: string) {
        if (!dateStr) return "";
        const d = new Date(dateStr);
        return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
    }

    // Timeline column helper mapping dates to indexes
    function getMonthIndex(dateStr: string): number {
        if (!dateStr || months.length === 0) return -1;
        const targetKey = getMonthKey(dateStr);
        return months.findIndex((m) => getMonthKey(m.date) === targetKey);
    }

    function getActualEndIndex(asset: any, subAsset?: any): number {
        if (months.length === 0) return -1;
        const parentName = getName(asset);
        const saName = subAsset ? subAsset.name : null;
        
        for (let i = 0; i < months.length; i++) {
            const m = months[i];
            
            // Check assets breakdown for dumps/leftovers/payouts
            if (m.breakdown?.assets) {
                for (const entry of m.breakdown.assets) {
                    const entryName = entry.name || "";
                    if (saName) {
                        if (entryName.includes(parentName) && entryName.includes(saName) && 
                            (entryName.includes("Dump") || entryName.includes("Leftover") || entryName.includes("Payout"))) {
                            return i;
                        }
                    } else {
                        if (entryName.includes(parentName) && 
                            (entryName.includes("Dump") || entryName.includes("Leftover") || entryName.includes("Payout"))) {
                            const subAssets = getSubAssets(asset);
                            const containsSubAsset = subAssets.some((sa: any) => entryName.includes(sa.name));
                            if (!containsSubAsset) {
                                return i;
                            }
                        }
                    }
                }
            }

            // Also check incomes breakdown for sub-asset payouts that don't dump into a loan
            if (m.breakdown?.incomes) {
                for (const entry of m.breakdown.incomes) {
                    const entryName = entry.name || "";
                    if (saName) {
                        if (entryName.includes(parentName) && entryName.includes(saName) && 
                            (entryName.includes("Dump") || entryName.includes("Leftover") || entryName.includes("Payout"))) {
                            return i;
                        }
                    }
                }
            }
        }
        return -1;
    }

    function getAssetColSpan(startDateStr: string, endDateStr: string, actualEndIdx?: number) {
        if (months.length === 0) return { startIdx: 0, endIdx: 0, visible: false };
        let startIdx = getMonthIndex(startDateStr);
        if (startIdx === -1) {
            const firstMonthDate = new Date(months[0].date);
            const start = new Date(startDateStr);
            if (start < firstMonthDate) {
                startIdx = 0;
            } else {
                return { startIdx: 0, endIdx: 0, visible: false }; // outside timeline
            }
        }
        
        let endIdx = getMonthIndex(endDateStr);
        if (endIdx === -1) {
            endIdx = months.length - 1; // ongoing
        }
        
        if (actualEndIdx !== undefined && actualEndIdx !== -1) {
            endIdx = Math.min(endIdx, actualEndIdx);
        }
        
        return { startIdx, endIdx, visible: true };
    }

    function getLoanActualEndIndex(loan: any): number {
        if (months.length === 0) return -1;
        const loanName = getName(loan);
        
        let lastActiveIdx = -1;
        for (let i = 0; i < months.length; i++) {
            const m = months[i];
            const loanBreak = m.breakdown?.loans?.find((l: any) => l.entityName === loanName || l.name?.includes(loanName));
            if (loanBreak) {
                lastActiveIdx = i;
                if (loanBreak.balance <= 0.05) {
                    return i;
                }
            }
        }
        return lastActiveIdx;
    }

    function getLoanColSpan(loan: any, actualEndIdx?: number) {
        if (months.length === 0) return { startIdx: 0, endIdx: 0, visible: false };
        const startDateStr = getActiveVersion(loan)?.startDate || "";
        let startIdx = getMonthIndex(startDateStr);
        if (startIdx === -1) {
            const firstMonthDate = new Date(months[0].date);
            const start = new Date(startDateStr);
            if (start < firstMonthDate) {
                startIdx = 0;
            } else {
                return { startIdx: 0, endIdx: 0, visible: false };
            }
        }
        
        let endIdx = months.length - 1;
        if (actualEndIdx !== undefined && actualEndIdx !== -1) {
            endIdx = Math.min(endIdx, actualEndIdx);
        } else {
            const runtime = getActiveVersion(loan)?.runtimeMonths || 0;
            endIdx = Math.min(months.length - 1, startIdx + runtime - 1);
        }
        
        return { startIdx, endIdx, visible: true };
    }

    // --- Loading Data & Projection Run ---
    async function fetchAllData() {
        isLoading = true;
        error = null;
        try {
            const [scResp, asResp, lnResp, incResp, blResp, expResp, vaResp] = await Promise.all([
                wsCall("scenarios::list", null, null, [ScenarioListSchema]).one(),
                wsCall("assets::list", null, null, [AssetListSchema]).one(),
                wsCall("loans::list", null, null, [LoanListSchema]).one(),
                wsCall("incomes::list", null, null, [IncomeListSchema]).one(),
                wsCall("bills::list", null, null, [BillListSchema]).one(),
                wsCall("expenses::list", null, null, [ExpenseListSchema]).one(),
                wsCall("virtualaccounts::list", null, null, [VirtualAccountListSchema]).one(),
            ]);

            if (scResp[1]) throw scResp[1];
            if (asResp[1]) throw asResp[1];
            if (lnResp[1]) throw lnResp[1];
            if (incResp[1]) throw incResp[1];
            if (blResp[1]) throw blResp[1];
            if (expResp[1]) throw expResp[1];
            if (vaResp[1]) throw vaResp[1];

            scenarios = (scResp[0]?.scenarios ?? []) as Scenario[];
            allAssets = asResp[0]?.assets ?? [];
            allLoans = lnResp[0]?.loans ?? [];
            allIncomes = incResp[0]?.incomes ?? [];
            allBills = blResp[0]?.bills ?? [];
            allExpenses = expResp[0]?.expenses ?? [];
            allVirtualAccounts = vaResp[0]?.virtualAccounts ?? [];

            if (scenarios.length > 0) {
                const preferredId = localStorage.getItem("timeline_scenario_id") || scenarios[0].id || null;
                selectedScenarioId = scenarios.some(s => s.id === preferredId) ? preferredId : scenarios[0].id || null;
                if (selectedScenarioId) {
                    await handleScenarioChange(selectedScenarioId);
                }
            }
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function runProjection() {
        if (!selectedScenarioId || !activeScenario) return;
        isProjecting = true;
        months = [];

        try {
            const callResult = wsCall(
                "scenarios::projection",
                ScenarioSchema,
                { id: selectedScenarioId, projectionMonths },
                [
                    ProjectionMonthSchema,
                    YieldMapSchema,
                    PerformanceMetricsSchema,
                    ErrorSchema,
                ]
            );

            for await (const [message, err] of callResult.many()) {
                if (err) throw err;
                if (message) {
                    if (message.$typeName === "api.ProjectionMonth") {
                        months.push(message);
                    } else if (message.$typeName === "api.YieldMap") {
                        simulatedYields = { ...((message as any).yields || {}) };
                    }
                }
            }
            months = [...months]; // Trigger Svelte updates
        } catch (err: any) {
            console.error("Failed to run timeline projection", err);
            error = err.message;
        } finally {
            isProjecting = false;
        }
    }

    // --- Sub-Asset Resizing Handlers ---
    function startResizingSubAsset(e: MouseEvent, asset: any, sa: any, side: 'start' | 'end') {
        e.stopPropagation();
        e.preventDefault();
        
        const saSpan = getAssetColSpan(sa.startDate, sa.endDate);
        if (!saSpan.visible) return;

        resizingSubAssetData = {
            asset,
            sa,
            side,
            originalStartIdx: saSpan.startIdx,
            originalEndIdx: saSpan.endIdx,
            currentStartIdx: saSpan.startIdx,
            currentEndIdx: saSpan.endIdx
        };
        isResizingSubAsset = true;

        window.addEventListener('mousemove', handleSubAssetResizeMove);
        window.addEventListener('mouseup', handleSubAssetResizeEnd);
    }

    function handleSubAssetResizeMove(e: MouseEvent) {
        if (!isResizingSubAsset || !resizingSubAssetData || !timelineScrollContainer) return;

        const rect = timelineScrollContainer.getBoundingClientRect();
        const scrollOffset = timelineScrollContainer.scrollLeft;
        const relativeX = e.clientX - rect.left + scrollOffset;
        
        // Month width is 256px
        let newIdx = Math.floor(relativeX / 256);
        newIdx = Math.max(0, Math.min(newIdx, months.length - 1));

        if (resizingSubAssetData.side === 'start') {
            resizingSubAssetData.currentStartIdx = Math.min(newIdx, resizingSubAssetData.originalEndIdx);
        } else {
            resizingSubAssetData.currentEndIdx = Math.max(newIdx, resizingSubAssetData.originalStartIdx);
        }
    }

    async function handleSubAssetResizeEnd() {
        if (!isResizingSubAsset || !resizingSubAssetData) return;

        const { asset, sa, currentStartIdx, currentEndIdx, originalStartIdx, originalEndIdx } = resizingSubAssetData;
        
        window.removeEventListener('mousemove', handleSubAssetResizeMove);
        window.removeEventListener('mouseup', handleSubAssetResizeEnd);
        
        isResizingSubAsset = false;

        if (currentStartIdx !== originalStartIdx || currentEndIdx !== originalEndIdx) {
            const startDate = months[currentStartIdx].date;
            const endDate = months[currentEndIdx].date;
            
            // Calculate new monthly amount
            const monthSpan = currentEndIdx - currentStartIdx + 1;
            const targetValue = Number(sa.targetValue) || 0;
            const newAmountPerMonth = monthSpan > 0 ? targetValue / monthSpan : targetValue;

            // Update the sub-asset in the local state first for immediate UI feedback
            const av = getActiveVersion(asset);
            const subAssets = av?.subAssets || [];
            const targetSa = subAssets.find((s: any) => s.id === sa.id);
            if (targetSa) {
                targetSa.startDate = startDate;
                targetSa.endDate = endDate;
                targetSa.amountPerMonth = newAmountPerMonth;
            }

            // Save to backend
            await persistAssetChanges(asset);
        }

        resizingSubAssetData = null;
    }

    async function persistAssetChanges(asset: any) {
        isProjecting = true;
        try {
            const av = getActiveVersion(asset);
            
            // We need to map it correctly for the backend
            const payload = {
                id: asset.id || "",
                name: asset.name,
                poolId: asset.poolId || "",
                accountIds: asset.accountIds || [],
                linkToScenarios: asset.linkToScenarios || [],
                activeVersion: {
                    id: av.id || "",
                    assetId: av.assetId || "",
                    type: av.type || "STOCKS",
                    targetValue: parseFloat(av.targetValue) || 0,
                    dumpingLoanId: av.dumpingLoanId || "",
                    stopModificationId: av.stopModificationId || "",
                    interestRate: parseFloat(av.interestRate) || 0,
                    interestInterval: av.interestInterval || "YEARLY",
                    amountPerMonth: parseFloat(av.amountPerMonth) || 0,
                    remainderStartDate: av.remainderStartDate || "",
                    startDate: av.startDate || "",
                    endDate: av.endDate || "",
                    useForPassiveIncome: !!av.useForPassiveIncome,
                    etfConfig: (av.etfConfig || []).map((t: any) => ({
                        tracker: t.tracker || "",
                        historicalTracker: t.historicalTracker || "",
                        conversionTracker: t.conversionTracker || "",
                        historyProvider: t.historyProvider || "",
                        percentage: parseFloat(t.percentage) || 0,
                        ter: parseFloat(t.ter) || 0,
                        stitchingSegments: (t.stitchingSegments || []).map((seg: any) => ({
                            provider: seg.provider || "",
                            lookupTicker: seg.lookupTicker || "",
                            conversionTracker: seg.conversionTracker || "",
                        })),
                    })),
                    penalties: (av.penalties || []).map((p: any) => ({
                        name: p.name || "",
                        triggerType: p.triggerType || "",
                        percentage: parseFloat(p.percentage) || 0,
                    })),
                    subAssets: (av.subAssets || []).map((s: any) => ({
                        id: s.id || "",
                        name: s.name || "",
                        targetValue: Number(s.targetValue) || 0,
                        amountPerMonth: Number(s.amountPerMonth) || 0,
                        isRemainderConsumer: !!s.isRemainderConsumer,
                        remainderStartDate: s.remainderStartDate || "",
                        dumpingLoanId: s.dumpingLoanId || "",
                        startDate: s.startDate || "",
                        endDate: s.endDate || "",
                        earliestDumpDate: s.earliestDumpDate || "",
                        expenseId: s.expenseId || "",
                        remainderPriority: Number(s.remainderPriority) || 0,
                    }))
                }
            };

            await wsCall("assets::save", AssetSchema, payload, [AssetSchema]).one();
            
            // Re-fetch Assets and run projection
            const [asResp] = await wsCall("assets::list", null, null, [AssetListSchema]).one();
            if (asResp) {
                allAssets = asResp.assets ?? [];
            }
            await runProjection();
        } catch (err: any) {
            console.error("Failed to persist asset changes", err);
            error = "Failed to save changes: " + err.message;
        } finally {
            isProjecting = false;
        }
    }

    async function saveScenarioAndReProject() {
        if (!activeScenario) return;
        isProjecting = true;
        try {
            const [saved, err] = await wsCall(
                "scenarios::save",
                ScenarioSchema,
                {
                    id: activeScenario.id || "",
                    name: activeScenario.name,
                    description: activeScenario.description,
                    projectionMonths: activeScenario.projectionMonths,
                    remainderOrder: activeScenario.remainderOrder,
                    isActive: activeScenario.isActive,
                    monthStartDay: activeScenario.monthStartDay,
                    startDate: activeScenario.startDate,
                    entities: (activeScenario.entities || []).map((e) => ({
                        entityId: e.entityId || "",
                        entityType: e.entityType || "",
                        versionId: e.versionId || "",
                    })),
                    simulations: activeScenario.simulations,
                    simYears: activeScenario.simYears,
                    simPercent: activeScenario.simPercent,
                    lookbackYears: activeScenario.lookbackYears,
                    mcImplementation: activeScenario.mcImplementation,
                    passiveIncomePercentage: activeScenario.passiveIncomePercentage,
                    etfParams: Object.fromEntries(
                        Object.entries(activeScenario.etfParams || {}).map(([k, v]) => [
                            k,
                            {
                                simulations: Number(v.simulations || 0),
                                simYears: Number(v.simYears || 0),
                                simPercent: Number(v.simPercent || 0),
                                lookbackYears: Number(v.lookbackYears || 0),
                            }
                        ])
                    ),
                },
                [ScenarioSchema]
            ).one();

            if (err) throw err;
            activeScenario = saved as Scenario;
            await runProjection();
        } catch (err: any) {
            error = err.message;
        } finally {
            isProjecting = false;
        }
    }

    function prepareScenarioPayload(sc: any): any {
        if (!sc) return null;
        return {
            id: sc.id || "",
            name: sc.name || "",
            description: sc.description || "",
            projectionMonths: Number(sc.projectionMonths) || 24,
            remainderOrder: sc.remainderOrder || [],
            isActive: !!sc.isActive,
            monthStartDay: Number(sc.monthStartDay) || 1,
            startDate: sc.startDate || "",
            entities: (sc.entities || []).map((e: any) => ({
                entityId: e.entityId || "",
                entityType: e.entityType || "",
                versionId: e.versionId || "",
            })),
            simulations: Number(sc.simulations) || 0,
            simYears: Number(sc.simYears) || 0,
            simPercent: Number(sc.simPercent) || 0,
            lookbackYears: Number(sc.lookbackYears) || 0,
            mcImplementation: sc.mcImplementation || "",
            passiveIncomePercentage: Number(sc.passiveIncomePercentage) || 0,
            etfParams: Object.fromEntries(
                Object.entries(sc.etfParams || {}).map(([k, v]: [string, any]) => [
                    k,
                    {
                        simulations: Number(v.simulations || 0),
                        simYears: Number(v.simYears || 0),
                        simPercent: Number(v.simPercent || 0),
                        lookbackYears: Number(v.lookbackYears || 0),
                    }
                ])
            ),
        };
    }

    async function handleScenarioChange(id: string) {
        selectedScenarioId = id;
        localStorage.setItem("timeline_scenario_id", id);
        const sc = scenarios.find(s => s.id === id);
        if (sc) {
            activeScenario = JSON.parse(JSON.stringify(sc)); // Clone object
            projectionMonths = activeScenario?.projectionMonths || 24;
            optimizationResults = null;
            expenseOptimizationResults = null;
            await runProjection();
        }
    }

    // --- Rescheduling Expenses Drag & Drop & Direct Saves ---
    function handleDragStartExpense(e: DragEvent, expense: any) {
        if (!isFlexibleExpense(expense.name)) {
            alert("This is a fixed expense. Reschedule it in the details modal by switching it to 'Flexible' first.");
            e.preventDefault();
            return;
        }

        if (e.dataTransfer) {
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData("application/json", JSON.stringify(expense));
        }
    }

    function handleDragOverMonth(e: DragEvent, monthStr: string) {
        e.preventDefault();
        activeDragOverMonthStr = monthStr;
    }

    function handleDragLeaveMonth() {
        activeDragOverMonthStr = null;
    }

    async function handleDropExpense(e: DragEvent, targetMonthStr: string) {
        e.preventDefault();
        activeDragOverMonthStr = null;
        if (!e.dataTransfer || !activeScenario) return;

        try {
            const raw = e.dataTransfer.getData("application/json");
            if (!raw) return;
            const expense = JSON.parse(raw);

            const newDueDate = `${targetMonthStr}-01T00:00:00Z`;
            await updateExpenseDetails(expense, expense.name, newDueDate);
        } catch (err: any) {
            alert("Failed to reschedule expense: " + err.message);
        }
    }

    async function updateExpenseDetails(expense: Expense, newName: string, newDueDate: string) {
        isProjecting = true;
        try {
            const [, err] = await wsCall(
                "expenses::save",
                ExpenseSchema,
                {
                    id: expense.id || "",
                    name: newName,
                    poolId: expense.poolId || "",
                    accountIds: expense.accountIds || [],
                    activeVersion: {
                        id: expense.activeVersion?.id || "",
                        expenseId: expense.activeVersion?.expenseId || "",
                        amount: expense.activeVersion?.amount || 0,
                        dueDate: newDueDate,
                        slices: expense.activeVersion?.slices || [],
                        subExpenses: expense.activeVersion?.subExpenses || [],
                    },
                    linkToScenarios: expense.linkToScenarios || [],
                },
                [ErrorSchema]
            ).one();

            if (err) throw err;

            // If there is an associated sub-asset funding plan, update it as well
            let targetAsset = null;
            let targetSubAssetIdx = -1;
            for (const asset of allAssets) {
                const av = getActiveVersion(asset);
                const subAssets = av?.subAssets || [];
                const idx = subAssets.findIndex((sa: any) => sa.expenseId === expense.id);
                if (idx !== -1) {
                    targetAsset = asset;
                    targetSubAssetIdx = idx;
                    break;
                }
            }

            if (targetAsset && targetSubAssetIdx !== -1) {
                const av = getActiveVersion(targetAsset);
                const monthsLeft = Math.max(1, getMonthsBetween(new Date().toISOString(), newDueDate));
                const targetSub = av.subAssets[targetSubAssetIdx];
                const isRemainder = !!targetSub?.isRemainderConsumer;
                const amountNeededPerMonth = isRemainder ? 0 : ((expense.activeVersion?.amount || 0) / monthsLeft);
                
                // Deep copy and update sub-asset
                const updatedSubAssets = (av.subAssets || []).map((sa: any, idx: number) => {
                    if (idx === targetSubAssetIdx) {
                        return {
                            ...sa,
                            endDate: newDueDate,
                            amountPerMonth: amountNeededPerMonth,
                        };
                    }
                    return sa;
                });

                // Save updated asset
                const [, assetErr] = await wsCall(
                    "assets::save",
                    AssetSchema,
                    {
                        id: targetAsset.id || "",
                        name: targetAsset.name,
                        poolId: targetAsset.poolId || "",
                        accountIds: targetAsset.accountIds || [],
                        linkToScenarios: targetAsset.linkToScenarios || [],
                        activeVersion: {
                            id: av.id || "",
                            assetId: av.assetId || "",
                            type: av.type || "STOCKS",
                            targetValue: parseFloat(av.targetValue) || 0,
                            dumpingLoanId: av.dumpingLoanId || "",
                            stopModificationId: av.stopModificationId || "",
                            interestRate: parseFloat(av.interestRate) || 0,
                            interestInterval: av.interestInterval || "YEARLY",
                            amountPerMonth: parseFloat(av.amountPerMonth) || 0,
                            remainderStartDate: av.remainderStartDate || "",
                            startDate: av.startDate || "",
                            endDate: av.endDate || "",
                            useForPassiveIncome: !!av.useForPassiveIncome,
                            etfConfig: (av.etfConfig || []).map((t: any) => ({
                                tracker: t.tracker || "",
                                historicalTracker: t.historicalTracker || "",
                                conversionTracker: t.conversionTracker || "",
                                historyProvider: t.historyProvider || "",
                                percentage: parseFloat(t.percentage) || 0,
                                ter: parseFloat(t.ter) || 0,
                                stitchingSegments: (t.stitchingSegments || []).map((seg: any) => ({
                                    provider: seg.provider || "",
                                    lookupTicker: seg.lookupTicker || "",
                                    conversionTracker: seg.conversionTracker || "",
                                })),
                            })),
                            subAssets: updatedSubAssets.map((s: any) => ({
                                id: s.id || "",
                                name: s.name || "",
                                targetValue: Number(s.targetValue) || 0,
                                amountPerMonth: Number(s.amountPerMonth) || 0,
                                isRemainderConsumer: !!s.isRemainderConsumer,
                                remainderStartDate: s.remainderStartDate || "",
                                dumpingLoanId: s.dumpingLoanId || "",
                                startDate: s.startDate || "",
                                endDate: s.endDate || "",
                                earliestDumpDate: s.earliestDumpDate || "",
                                expenseId: s.expenseId || "",
                                remainderPriority: Number(s.remainderPriority) || 0,
                            })),
                        },
                    },
                    [ErrorSchema]
                ).one();

                if (assetErr) {
                    console.error("Failed to update associated sub-asset:", assetErr);
                } else {
                    // Refresh assets list
                    const [asResp] = await wsCall("assets::list", null, null, [AssetListSchema]).one();
                    if (asResp) {
                        allAssets = asResp.assets ?? [];
                    }
                }
            }

            // Reload expenses list and refresh timeline projection
            const [expResp] = await wsCall("expenses::list", null, null, [ExpenseListSchema]).one();
            if (expResp) {
                allExpenses = expResp.expenses ?? [];
            }
            await runProjection();
        } catch (err: any) {
            alert("Failed to update expense details: " + err.message);
        } finally {
            isProjecting = false;
        }
    }

    // --- Remainder Waterfall Drag & Drop ---
    function handleDragStartWaterfall(e: DragEvent, index: number) {
        draggedWaterfallIndex = index;
        if (e.dataTransfer) {
            e.dataTransfer.effectAllowed = "move";
        }
    }

    function handleDragOverWaterfall(e: DragEvent, index: number) {
        e.preventDefault();
        dragOverWaterfallIndex = index;
    }

    function handleDropWaterfall(e: DragEvent, targetIndex: number) {
        e.preventDefault();
        dragOverWaterfallIndex = null;
        if (draggedWaterfallIndex === null || !activeScenario) return;

        const updatedOrder = [...activeScenario.remainderOrder];
        const [movedId] = updatedOrder.splice(draggedWaterfallIndex, 1);
        updatedOrder.splice(targetIndex, 0, movedId);

        activeScenario.remainderOrder = updatedOrder;
        draggedWaterfallIndex = null;

        saveScenarioAndReProject();
    }

    function addToWaterfall(id: string) {
        if (!activeScenario) return;
        activeScenario.remainderOrder = [...activeScenario.remainderOrder, id];
        saveScenarioAndReProject();
    }

    function removeFromWaterfall(id: string) {
        if (!activeScenario) return;
        activeScenario.remainderOrder = activeScenario.remainderOrder.filter(item => item !== id);
        saveScenarioAndReProject();
    }

    // --- Sub-Asset Funding Plan Generator & Expense Detail Modal ---
    function prepareExpenseForEdit(expObj: any) {
        if (!expObj) return null;
        if (!expObj.activeVersion) {
            expObj.activeVersion = {
                id: "",
                expenseId: "",
                amount: 0,
                dueDate: new Date().toISOString(),
                slices: [],
                subExpenses: []
            };
        }
        if (!expObj.activeVersion.subExpenses) {
            expObj.activeVersion.subExpenses = [];
        }
        for (const sub of expObj.activeVersion.subExpenses) {
            sub.metadataList = [];
            if (sub.metadata) {
                for (const [key, val] of Object.entries(sub.metadata)) {
                    sub.metadataList.push({ key, value: val });
                }
            }
        }
        return expObj;
    }

    function addSubExpense() {
        if (!selectedExpenseObj.activeVersion) return;
        if (!selectedExpenseObj.activeVersion.subExpenses) {
            selectedExpenseObj.activeVersion.subExpenses = [];
        }
        selectedExpenseObj.activeVersion.subExpenses.push({
            id: "",
            description: "",
            amount: 0,
            metadata: {},
            metadataList: []
        });
    }

    function removeSubExpense(subIdx: number) {
        if (!selectedExpenseObj.activeVersion?.subExpenses) return;
        const subAmt = selectedExpenseObj.activeVersion.subExpenses[subIdx].amount || 0;
        selectedExpenseObj.activeVersion.subExpenses.splice(subIdx, 1);
        selectedExpenseObj.activeVersion.amount = Math.max(0, (selectedExpenseObj.activeVersion.amount || 0) - subAmt);
    }

    function updateSubExpenseAmount(subIdx: number, newAmount: number) {
        if (!selectedExpenseObj.activeVersion?.subExpenses) return;
        const prevAmount = selectedExpenseObj.activeVersion.subExpenses[subIdx].amount || 0;
        selectedExpenseObj.activeVersion.subExpenses[subIdx].amount = newAmount;
        selectedExpenseObj.activeVersion.amount = (selectedExpenseObj.activeVersion.amount || 0) - prevAmount + newAmount;
    }

    function addMetadataField(subIdx: number) {
        if (!selectedExpenseObj.activeVersion?.subExpenses) return;
        const sub = selectedExpenseObj.activeVersion.subExpenses[subIdx];
        if (!sub.metadataList) {
            sub.metadataList = [];
        }
        sub.metadataList.push({ key: "", value: "" });
    }

    function removeMetadataField(subIdx: number, pairIdx: number) {
        if (!selectedExpenseObj.activeVersion?.subExpenses) return;
        selectedExpenseObj.activeVersion.subExpenses[subIdx].metadataList.splice(pairIdx, 1);
    }

    function openExpenseDetails(exp: any, expObj: any) {
        selectedExpense = exp;
        selectedExpenseObj = prepareExpenseForEdit(expObj ? JSON.parse(JSON.stringify(expObj)) : {
            id: exp.entityName,
            name: exp.name,
            activeVersion: { amount: exp.amount, dueDate: exp.date || new Date().toISOString(), slices: [], subExpenses: [] }
        });
        modalMode = "edit";
        isFlexExpenseState = isFlexibleExpense(selectedExpenseObj.name);
        fundingSubAssetCreated = false;
        fundingMessage = null;
        fundingAssetId = activeAssets.length > 0 ? getID(activeAssets[0]) : "";

        // Check if there is an existing sub-asset linked to this expense that is a remainder consumer
        let hasRemainderConsumer = false;
        for (const asset of allAssets) {
            const subAssets = getSubAssets(asset);
            for (const sa of subAssets) {
                if (sa.expenseId === selectedExpenseObj.id && sa.isRemainderConsumer) {
                    hasRemainderConsumer = true;
                    fundingAssetId = getID(asset);
                    break;
                }
            }
        }
        fundingRemainderConsumer = hasRemainderConsumer;

        showExpenseModal = true;
    }

    function openNewExpenseModal(dateStr: string) {
        selectedExpense = null;
        selectedExpenseObj = prepareExpenseForEdit({
            id: "",
            name: "New Expense",
            poolId: "",
            accountIds: [],
            activeVersion: {
                id: "",
                expenseId: "",
                amount: 100,
                dueDate: dateStr,
                slices: [],
                subExpenses: []
            },
            linkToScenarios: activeScenario ? [activeScenario.id] : []
        });
        modalMode = "edit";
        isFlexExpenseState = false;
        fundingSubAssetCreated = false;
        fundingMessage = null;
        fundingAssetId = activeAssets.length > 0 ? getID(activeAssets[0]) : "";
        fundingRemainderConsumer = false;
        showExpenseModal = true;
    }


    async function toggleExpenseFlexibility() {
        if (!selectedExpenseObj) return;
        
        let newName = selectedExpenseObj.name;
        const isFlex = isFlexibleExpense(newName);
        
        if (isFlex) {
            newName = cleanExpenseName(newName);
        } else {
            newName = newName + " (Flexible)";
        }
        
        selectedExpenseObj.name = newName;
        isFlexExpenseState = !isFlex;
        
        if (selectedExpenseObj.id) {
            await updateExpenseDetails(selectedExpenseObj, newName, selectedExpenseObj.activeVersion?.dueDate || "");
            if (selectedExpense) {
                selectedExpense.name = newName;
            }
        }
    }

    async function saveExpenseChanges() {
        if (!selectedExpenseObj) return;
        isProjecting = true;
        try {
            // Ensure proper suffix based on flexibility state
            let finalName = selectedExpenseObj.name;
            const isFlex = isFlexibleExpense(finalName);
            if (isFlexExpenseState) {
                if (!isFlex) {
                    finalName = cleanExpenseName(finalName) + " (Flexible)";
                }
            } else {
                finalName = cleanExpenseName(finalName);
            }

            // If it's a new expense, link to current scenario
            if (!selectedExpenseObj.id) {
                selectedExpenseObj.linkToScenarios = activeScenario ? [activeScenario.id] : [];
            }

            const subExpensesMapped = (selectedExpenseObj.activeVersion?.subExpenses || []).map((sub: any) => {
                const metadata: Record<string, string> = {};
                if (sub.metadataList) {
                    for (const pair of sub.metadataList) {
                        if (pair.key && pair.key.trim() !== "") {
                            metadata[pair.key.trim()] = pair.value || "";
                        }
                    }
                }
                return {
                    id: sub.id || "",
                    description: sub.description || "",
                    amount: Number(sub.amount) || 0,
                    metadata: metadata
                };
            });

            const [saved, err] = await wsCall(
                "expenses::save",
                ExpenseSchema,
                {
                    id: selectedExpenseObj.id || "",
                    name: finalName,
                    poolId: selectedExpenseObj.poolId || "",
                    accountIds: selectedExpenseObj.accountIds || [],
                    activeVersion: {
                        id: selectedExpenseObj.activeVersion?.id || "",
                        expenseId: selectedExpenseObj.activeVersion?.expenseId || "",
                        amount: Number(selectedExpenseObj.activeVersion?.amount) || 0,
                        dueDate: selectedExpenseObj.activeVersion?.dueDate || "",
                        slices: selectedExpenseObj.activeVersion?.slices || [],
                        subExpenses: subExpensesMapped,
                    },
                    linkToScenarios: selectedExpenseObj.linkToScenarios || [],
                },
                [ErrorSchema]
            ).one();

            if (err) throw err;

            // Automatically add new expense to explicit scenario entities if configured
            if (!selectedExpenseObj.id && saved && activeScenario && activeScenario.entities && activeScenario.entities.length > 0) {
                const newExpId = saved.id || getID(saved);
                if (newExpId && !activeScenario.entities.some(e => e.entityId === newExpId && e.entityType === "EXPENSE")) {
                    activeScenario.entities.push({
                        entityId: newExpId,
                        entityType: "EXPENSE",
                        versionId: ""
                    });
                    await wsCall("scenarios::save", ScenarioSchema, prepareScenarioPayload(activeScenario), [ScenarioSchema]).one();
                }
            }

            // Re-fetch expenses list
            const [expResp] = await wsCall("expenses::list", null, null, [ExpenseListSchema]).one();
            if (expResp) {
                allExpenses = expResp.expenses ?? [];
            }

            await runProjection();
            showExpenseModal = false;
        } catch (e: any) {
            alert("Failed to save expense: " + e.message);
        } finally {
            isProjecting = false;
        }
    }

    async function createFundingPlan() {
        if (!selectedExpenseObj || !fundingAssetId) return;

        const selectedAsset = allAssets.find(a => getID(a) === fundingAssetId);
        if (!selectedAsset) return;

        // If funding via remainder, ensure the expense name has " (Flexible)"
        if (fundingRemainderConsumer && !isFlexibleExpense(selectedExpenseObj.name)) {
            const newName = cleanExpenseName(selectedExpenseObj.name) + " (Flexible)";
            selectedExpenseObj.name = newName;
            isFlexExpenseState = true;
            await updateExpenseDetails(selectedExpenseObj, newName, selectedExpenseObj.activeVersion?.dueDate || "");
        }

        const monthsLeft = Math.max(1, getMonthsBetween(new Date().toISOString(), selectedExpenseObj.activeVersion?.dueDate || ""));
        const amountNeededPerMonth = fundingRemainderConsumer ? 0 : (selectedExpenseObj.activeVersion!.amount / monthsLeft);

        // Generate SubAsset
        const newSubAsset = {
            id: "sa_" + Math.random().toString(36).substring(2, 11),
            name: `${cleanExpenseName(selectedExpenseObj.name)} Fund`,
            targetValue: selectedExpenseObj.activeVersion!.amount,
            amountPerMonth: amountNeededPerMonth,
            isRemainderConsumer: fundingRemainderConsumer,
            startDate: new Date().toISOString().substring(0, 7) + "-01T00:00:00Z",
            endDate: selectedExpenseObj.activeVersion?.dueDate || "",
            dumpingLoanId: "",
            remainderStartDate: "",
            earliestDumpDate: "",
            expenseId: selectedExpenseObj.id,
            remainderPriority: 0,
        };

        const activeVersion = getActiveVersion(selectedAsset);
        if (selectedAsset.id && simulatedYields[selectedAsset.id] !== undefined) {
            activeVersion.interestRate = simulatedYields[selectedAsset.id];
        }
        const existingSubAssets = activeVersion.subAssets || [];


        // Build Payload
        const payloadSubAssets = [
            ...existingSubAssets.map((s: any) => ({
                id: s.id || "",
                name: s.name || "",
                targetValue: Number(s.targetValue) || 0,
                amountPerMonth: Number(s.amountPerMonth) || 0,
                isRemainderConsumer: !!s.isRemainderConsumer,
                remainderStartDate: s.remainderStartDate || "",
                dumpingLoanId: s.dumpingLoanId || "",
                startDate: s.startDate || "",
                endDate: s.endDate || "",
                earliestDumpDate: s.earliestDumpDate || "",
                expenseId: s.expenseId || "",
                remainderPriority: Number(s.remainderPriority) || 0,
            })),
            {
                ...newSubAsset,
                targetValue: Number(newSubAsset.targetValue),
                amountPerMonth: Number(newSubAsset.amountPerMonth)
            }
        ];

        try {
            isProjecting = true;
            const [, err] = await wsCall(
                "assets::save",
                AssetSchema,
                {
                    id: selectedAsset.id || "",
                    name: selectedAsset.name,
                    poolId: selectedAsset.poolId || "",
                    accountIds: selectedAsset.accountIds || [],
                    linkToScenarios: selectedAsset.linkToScenarios || [],
                    activeVersion: {
                        id: activeVersion.id || "",
                        assetId: activeVersion.assetId || "",
                        type: activeVersion.type || "STOCKS",
                        targetValue: parseFloat(activeVersion.targetValue) || 0,
                        dumpingLoanId: activeVersion.dumpingLoanId || "",
                        stopModificationId: activeVersion.stopModificationId || "",
                        interestRate: parseFloat(activeVersion.interestRate) || 0,
                        interestInterval: activeVersion.interestInterval || "YEARLY",
                        amountPerMonth: parseFloat(activeVersion.amountPerMonth) || 0,
                        remainderStartDate: activeVersion.remainderStartDate || "",
                        startDate: activeVersion.startDate || "",
                        endDate: activeVersion.endDate || "",
                        useForPassiveIncome: !!activeVersion.useForPassiveIncome,
                        etfConfig: (activeVersion.etfConfig || []).map((t: any) => ({
                            tracker: t.tracker || "",
                            historicalTracker: t.historicalTracker || "",
                            conversionTracker: t.conversionTracker || "",
                            historyProvider: t.historyProvider || "",
                            percentage: parseFloat(t.percentage) || 0,
                            ter: parseFloat(t.ter) || 0,
                            stitchingSegments: (t.stitchingSegments || []).map((seg: any) => ({
                                provider: seg.provider || "",
                                lookupTicker: seg.lookupTicker || "",
                                conversionTracker: seg.conversionTracker || "",
                            })),
                        })),
                        penalties: (activeVersion.penalties || []).map((p: any) => ({
                            name: p.name || "",
                            triggerType: p.triggerType || "",
                            percentage: parseFloat(p.percentage) || 0,
                        })),
                        subAssets: payloadSubAssets,
                    },
                },
                [AssetSchema]
            ).one();

            if (err) throw err;

            // Automatically add new sub-asset to active scenario if it has explicit entities
            if (activeScenario && activeScenario.entities && activeScenario.entities.length > 0) {
                if (!activeScenario.entities.some(e => e.entityId === newSubAsset.id && e.entityType === "SUB_ASSET")) {
                    activeScenario.entities.push({
                        entityId: newSubAsset.id,
                        entityType: "SUB_ASSET",
                        versionId: ""
                    });
                    await wsCall("scenarios::save", ScenarioSchema, prepareScenarioPayload(activeScenario), [ScenarioSchema]).one();
                }
            }

            // Re-fetch Assets and run projection to see the updated savings plan
            const [asResp] = await wsCall("assets::list", null, null, [AssetListSchema]).one();
            if (asResp) {
                allAssets = asResp.assets ?? [];
            }
            await runProjection();
            
            fundingSubAssetCreated = true;
            fundingMessage = `Success! Added savings sub-asset "${newSubAsset.name}" to Asset "${selectedAsset.name}". Saving ${formatCurrency(amountNeededPerMonth)}/month until ${parseMonthYear(newSubAsset.endDate)}.`;
        } catch (e: any) {
            alert("Failed to create funding sub-asset: " + e.message);
        } finally {
            isProjecting = false;
        }
    }

    // --- Asset Editor Modal Logic ---
    function openAssetDetails(asset: any) {
        editingAsset = asset;
        editingAssetObj = JSON.parse(JSON.stringify(asset));
        if (!editingAssetObj.activeVersion) {
            editingAssetObj.activeVersion = {};
        }
        if (!editingAssetObj.activeVersion.subAssets) {
            editingAssetObj.activeVersion.subAssets = [];
        }
        assetSaveError = null;
        showAssetModal = true;
    }

    function addEditingSubAsset() {
        if (!editingAssetObj.activeVersion.subAssets) {
            editingAssetObj.activeVersion.subAssets = [];
        }
        editingAssetObj.activeVersion.subAssets.push({
            id: "sa_" + Math.random().toString(36).substring(2, 11),
            name: "New Savings Pocket",
            targetValue: 0,
            amountPerMonth: 0,
            isRemainderConsumer: false,
            startDate: new Date().toISOString().substring(0, 7) + "-01T00:00:00Z",
            endDate: new Date(Date.now() + 365*24*60*60*1000).toISOString().substring(0, 7) + "-01T00:00:00Z",
            dumpingLoanId: "",
            remainderStartDate: "",
            earliestDumpDate: "",
        });
    }

    function removeEditingSubAsset(index: number) {
        editingAssetObj.activeVersion.subAssets.splice(index, 1);
    }

    async function saveAssetChanges() {
        if (!editingAssetObj) return;
        isProjecting = true;
        assetSaveError = null;

        try {
            const activeVersion = editingAssetObj.activeVersion;
            if (editingAssetObj.id && simulatedYields[editingAssetObj.id] !== undefined) {
                activeVersion.interestRate = simulatedYields[editingAssetObj.id];
            }

            // Find newly created sub assets
            const originalSubAssetIds = new Set(
                (editingAsset?.activeVersion?.subAssets || []).map((s: any) => s.id || "")
            );
            const newSubAssets = (activeVersion.subAssets || []).filter(
                (s: any) => s.id && !originalSubAssetIds.has(s.id)
            );

            const [, err] = await wsCall(
                "assets::save",
                AssetSchema,
                {
                    id: editingAssetObj.id || "",
                    name: editingAssetObj.name,
                    poolId: editingAssetObj.poolId || "",
                    accountIds: editingAssetObj.accountIds || [],
                    linkToScenarios: editingAssetObj.linkToScenarios || [],
                    activeVersion: {
                        id: activeVersion.id || "",
                        assetId: activeVersion.assetId || "",
                        type: activeVersion.type || "STOCKS",
                        targetValue: parseFloat(activeVersion.targetValue) || 0,
                        dumpingLoanId: activeVersion.dumpingLoanId || "",
                        stopModificationId: activeVersion.stopModificationId || "",
                        interestRate: parseFloat(activeVersion.interestRate) || 0,
                        interestInterval: activeVersion.interestInterval || "YEARLY",
                        amountPerMonth: parseFloat(activeVersion.amountPerMonth) || 0,
                        remainderStartDate: activeVersion.remainderStartDate || "",
                        startDate: activeVersion.startDate || "",
                        endDate: activeVersion.endDate || "",
                        useForPassiveIncome: !!activeVersion.useForPassiveIncome,
                        etfConfig: (activeVersion.etfConfig || []).map((t: any) => ({
                            tracker: t.tracker || "",
                            historicalTracker: t.historicalTracker || "",
                            conversionTracker: t.conversionTracker || "",
                            historyProvider: t.historyProvider || "",
                            percentage: parseFloat(t.percentage) || 0,
                            ter: parseFloat(t.ter) || 0,
                            stitchingSegments: (t.stitchingSegments || []).map((seg: any) => ({
                                provider: seg.provider || "",
                                lookupTicker: seg.lookupTicker || "",
                                conversionTracker: seg.conversionTracker || "",
                            })),
                        })),
                        penalties: (activeVersion.penalties || []).map((p: any) => ({
                            name: p.name || "",
                            triggerType: p.triggerType || "",
                            percentage: parseFloat(p.percentage) || 0,
                        })),
                        subAssets: (activeVersion.subAssets || []).map((s: any) => ({
                            id: s.id || "",
                            name: s.name || "",
                            targetValue: Number(s.targetValue) || 0,
                            amountPerMonth: Number(s.amountPerMonth) || 0,
                            isRemainderConsumer: !!s.isRemainderConsumer,
                            remainderStartDate: s.remainderStartDate || "",
                            dumpingLoanId: s.dumpingLoanId || "",
                            startDate: s.startDate || "",
                            endDate: s.endDate || "",
                            earliestDumpDate: s.earliestDumpDate || "",
                            expenseId: s.expenseId || "",
                            remainderPriority: Number(s.remainderPriority) || 0,
                        })),
                    },
                },
                [AssetSchema]
            ).one();

            if (err) throw err;

            // Automatically add new sub-assets to active scenario if it has explicit entities
            if (newSubAssets.length > 0 && activeScenario && activeScenario.entities && activeScenario.entities.length > 0) {
                const updatedEntities = [...activeScenario.entities];
                let changed = false;
                for (const sa of newSubAssets) {
                    if (!updatedEntities.some(e => e.entityId === sa.id && e.entityType === "SUB_ASSET")) {
                        updatedEntities.push({
                            entityId: sa.id,
                            entityType: "SUB_ASSET",
                            versionId: ""
                        });
                        changed = true;
                    }
                }
                if (changed) {
                    activeScenario.entities = updatedEntities;
                    await wsCall("scenarios::save", ScenarioSchema, prepareScenarioPayload(activeScenario), [ScenarioSchema]).one();
                }
            }

            // Re-fetch assets list and trigger projection
            const [asResp] = await wsCall("assets::list", null, null, [AssetListSchema]).one();
            if (asResp) {
                allAssets = asResp.assets ?? [];
            }
            await runProjection();
            showAssetModal = false;
        } catch (e: any) {
            assetSaveError = e.message;
        } finally {
            isProjecting = false;
        }
    }

    // --- Silent Simulations for Auto-Optimizer ---
    async function runSingleSimulationSilent(tempOrder: string[]): Promise<any[] | null> {
        if (!activeScenario) return null;
        try {
            const testScenario = prepareScenarioPayload({
                ...activeScenario,
                remainderOrder: tempOrder,
                projectionMonths
            });
            const callResult = wsCall("scenarios::projection", ScenarioSchema, testScenario, [
                ProjectionMonthSchema, YieldMapSchema, PerformanceMetricsSchema, ErrorSchema,
            ]);

            const testMonths: any[] = [];
            for await (const [message, err] of callResult.many()) {
                if (err) throw err;
                if (message && message.$typeName === "api.ProjectionMonth") {
                    testMonths.push(message);
                }
            }
            return testMonths;
        } catch (e) {
            console.error("Silent projection failed", e);
            return null;
        }
    }

    async function runSilentExpensesSimulation(expensesToTest: any[]): Promise<any[] | null> {
        if (!activeScenario) return null;
        try {
            for (const exp of expensesToTest) {
                await wsCall("expenses::save", ExpenseSchema, exp, [ErrorSchema]).one();
            }

            const callResult = wsCall("scenarios::projection", ScenarioSchema, { id: selectedScenarioId, projectionMonths }, [
                ProjectionMonthSchema, YieldMapSchema, PerformanceMetricsSchema, ErrorSchema,
            ]);

            const testMonths: any[] = [];
            for await (const [message, err] of callResult.many()) {
                if (err) throw err;
                if (message && message.$typeName === "api.ProjectionMonth") {
                    testMonths.push(message);
                }
            }
            return testMonths;
        } catch (e) {
            console.error("Silent expense simulation failed", e);
            return null;
        }
    }

    // --- Optimization Sorters (Waterfall & Rescheduling) ---
    async function optimizeRemainderOrder() {
        if (!activeScenario) return;
        isOptimizing = true;
        optimizationResults = null;

        const originalOrder = [...activeScenario.remainderOrder];

        try {
            const avalancheOrder = [
                ...[...activeLoans].sort((a, b) => getInterestRate(b) - getInterestRate(a)).map(getID),
                ...[...activeAssets].sort((a, b) => getInterestRate(b) - getInterestRate(a)).map(getID)
            ];

            const snowballOrder = [
                ...[...activeLoans].sort((a, b) => getBalance(a) - getBalance(b)).map(getID),
                ...[...activeAssets].sort((a, b) => getInterestRate(b) - getInterestRate(a)).map(getID)
            ];

            const mixedItems = [
                ...activeLoans.map(l => ({ id: getID(l), rate: getInterestRate(l) })),
                ...activeAssets.map(a => ({ id: getID(a), rate: getInterestRate(a) }))
            ].sort((a, b) => b.rate - a.rate).map(x => x.id);

            const assetsFirstOrder = [
                ...[...activeAssets].sort((a, b) => getInterestRate(b) - getInterestRate(a)).map(getID),
                ...[...activeLoans].sort((a, b) => getInterestRate(b) - getInterestRate(a)).map(getID)
            ];

            const strategies = [
                { name: "Avalanche Method (High Interest first)", order: avalancheOrder, desc: "Prioritizes high cost debt payoff, then high expected return investing. Mathematically optimal." },
                { name: "Snowball Method (Low Balance first)", order: snowballOrder, desc: "Prioritizes paying off small loans first for psychological wins." },
                { name: "High-Yield First (Mixed Priority)", order: mixedItems, desc: "Mixes loans and assets directly, allocating cash purely to highest rates (whether loan or investment)." },
                { name: "Assets First (Growth Focused)", order: assetsFirstOrder, desc: "Prioritizes building investment nodes first before liquidating debt balances." },
            ];

            const results = [];
            for (const strat of strategies) {
                activeScenario.remainderOrder = strat.order;
                await wsCall("scenarios::save", ScenarioSchema, prepareScenarioPayload(activeScenario), [ScenarioSchema]).one();

                const simulatedMonths = await runSingleSimulationSilent(strat.order);
                if (simulatedMonths && simulatedMonths.length > 0) {
                    const finalMonth = simulatedMonths[simulatedMonths.length - 1];
                    const finalNetWorth = finalMonth.balance + finalMonth.assetWorth - finalMonth.loanDebt;

                    let totalInterestPaid = 0;
                    for (const m of simulatedMonths) {
                        if (m.breakdown?.loans) {
                            for (const lBreak of m.breakdown.loans) {
                                totalInterestPaid += lBreak.interest || 0;
                            }
                        }
                    }

                    let payoffMonthStr = "Never";
                    for (const m of simulatedMonths) {
                        if (m.loanDebt <= 0.05) {
                            payoffMonthStr = parseMonthYear(m.date);
                            break;
                        }
                    }

                    results.push({
                        name: strat.name,
                        desc: strat.desc,
                        order: strat.order,
                        netWorth: finalNetWorth,
                        interestPaid: totalInterestPaid,
                        payoffMonth: payoffMonthStr,
                        isBest: false,
                    });
                }
            }

            if (results.length > 0) {
                let bestIdx = 0;
                let maxNW = results[0].netWorth;
                for (let i = 1; i < results.length; i++) {
                    if (results[i].netWorth > maxNW) {
                        maxNW = results[i].netWorth;
                        bestIdx = i;
                    }
                }
                results[bestIdx].isBest = true;
            }

            optimizationResults = results;
        } catch (err) {
            console.error("Optimization failed", err);
        } finally {
            // Restore original order
            activeScenario.remainderOrder = originalOrder;
            await wsCall("scenarios::save", ScenarioSchema, prepareScenarioPayload(activeScenario), [ScenarioSchema]).one();
            isOptimizing = false;
        }
    }

    async function optimizeFlexibleExpenses() {
        const flexibleExps = activeExpenses.filter(e => isFlexibleExpense(getName(e)));
        if (flexibleExps.length === 0) {
            alert("No flexible expenses found. Mark an expense as 'Flexible' in the timeline to test optimization!");
            return;
        }

        isOptimizing = true;
        expenseOptimizationResults = null;

        // Save original due dates to restore
        const originalDates = flexibleExps.map(e => ({
            id: e.id,
            dueDate: getActiveVersion(e)?.dueDate
        }));

        try {
            const baselineMonths = months;
            
            // Strategy 1: Delay until Debt-Free
            let debtPayoffMonthIdx = -1;
            for (let i = 0; i < baselineMonths.length; i++) {
                if (baselineMonths[i].loanDebt <= 0.05) {
                    debtPayoffMonthIdx = i;
                    break;
                }
            }

            let payoffTargetMonthStr = baselineMonths[baselineMonths.length - 1]?.date;
            if (debtPayoffMonthIdx !== -1 && debtPayoffMonthIdx < baselineMonths.length - 1) {
                payoffTargetMonthStr = baselineMonths[debtPayoffMonthIdx + 1].date; // month after payoff
            }

            const payoffAlignedExpenses = flexibleExps.map(e => ({
                id: getID(e),
                name: getName(e),
                poolId: e.poolId || "",
                accountIds: e.accountIds || [],
                linkToScenarios: e.linkToScenarios || [],
                activeVersion: {
                    id: getActiveVersion(e)?.id || "",
                    expenseId: getActiveVersion(e)?.expenseId || "",
                    amount: getActiveVersion(e)?.amount || 0,
                    dueDate: payoffTargetMonthStr,
                    slices: getActiveVersion(e)?.slices || []
                }
            }));

            // Strategy 2: Surplus Placement (Move each to month with highest remainder cash)
            const sortedBySurplus = [...baselineMonths].sort((a, b) => b.remainder - a.remainder);
            const bestSurplusMonthStr = sortedBySurplus[0]?.date || new Date().toISOString();

            const surplusAlignedExpenses = flexibleExps.map(e => ({
                id: getID(e),
                name: getName(e),
                poolId: e.poolId || "",
                accountIds: e.accountIds || [],
                linkToScenarios: e.linkToScenarios || [],
                activeVersion: {
                    id: getActiveVersion(e)?.id || "",
                    expenseId: getActiveVersion(e)?.expenseId || "",
                    amount: getActiveVersion(e)?.amount || 0,
                    dueDate: bestSurplusMonthStr,
                    slices: getActiveVersion(e)?.slices || []
                }
            }));

            const strategies = [
                { name: "Debt-Free Delay", desc: `Postpone flexible costs until after debt is liquidated (placed in ${parseMonthYear(payoffTargetMonthStr)}).`, configs: payoffAlignedExpenses },
                { name: "Surplus Placement", desc: `Schedule flexible costs on month with highest cash surplus (placed in ${parseMonthYear(bestSurplusMonthStr)}).`, configs: surplusAlignedExpenses }
            ];

            const results = [];
            for (const strat of strategies) {
                const simulatedMonths = await runSilentExpensesSimulation(strat.configs);
                if (simulatedMonths && simulatedMonths.length > 0) {
                    const finalMonth = simulatedMonths[simulatedMonths.length - 1];
                    const finalNetWorth = finalMonth.balance + finalMonth.assetWorth - finalMonth.loanDebt;

                    let totalInterestPaid = 0;
                    for (const m of simulatedMonths) {
                        if (m.breakdown?.loans) {
                            for (const lBreak of m.breakdown.loans) {
                                totalInterestPaid += lBreak.interest || 0;
                            }
                        }
                    }

                    results.push({
                        name: strat.name,
                        desc: strat.desc,
                        configs: strat.configs,
                        netWorth: finalNetWorth,
                        interestPaid: totalInterestPaid,
                        isBest: false
                    });
                }
            }

            if (results.length > 0) {
                let bestIdx = 0;
                let maxNW = results[0].netWorth;
                for (let i = 1; i < results.length; i++) {
                    if (results[i].netWorth > maxNW) {
                        maxNW = results[i].netWorth;
                        bestIdx = i;
                    }
                }
                results[bestIdx].isBest = true;
            }

            expenseOptimizationResults = results;
        } catch (e) {
            console.error("Expense optimization failed", e);
        } finally {
            // Restore original dates in DB
            for (const orig of originalDates) {
                const expObj = allExpenses.find(e => getID(e) === orig.id);
                if (expObj) {
                    await wsCall("expenses::save", ExpenseSchema, {
                        id: getID(expObj),
                        name: getName(expObj),
                        poolId: expObj.poolId || "",
                        accountIds: expObj.accountIds || [],
                        linkToScenarios: expObj.linkToScenarios || [],
                        activeVersion: {
                            id: getActiveVersion(expObj)?.id || "",
                            expenseId: getActiveVersion(expObj)?.expenseId || "",
                            amount: getActiveVersion(expObj)?.amount || 0,
                            dueDate: orig.dueDate,
                            slices: getActiveVersion(expObj)?.slices || [],
                            subExpenses: getActiveVersion(expObj)?.subExpenses || []
                        }
                    }, [ErrorSchema]).one();
                }
            }
            // Refresh local state and re-project
            const [expResp] = await wsCall("expenses::list", null, null, [ExpenseListSchema]).one();
            if (expResp) {
                allExpenses = expResp.expenses ?? [];
            }
            await runProjection();
            isOptimizing = false;
        }
    }

    async function applyExpenseRescheduling(configs: any[]) {
        isProjecting = true;
        try {
            for (const cfg of configs) {
                await wsCall("expenses::save", ExpenseSchema, cfg, [ErrorSchema]).one();
            }
            const [expResp] = await wsCall("expenses::list", null, null, [ExpenseListSchema]).one();
            if (expResp) {
                allExpenses = expResp.expenses ?? [];
            }
            await runProjection();
            expenseOptimizationResults = null;
        } catch (err: any) {
            alert(err.message);
        } finally {
            isProjecting = false;
        }
    }

    function applyRecommendation(recOrder: string[]) {
        if (!activeScenario) return;
        activeScenario.remainderOrder = [...recOrder];
        saveScenarioAndReProject();
        optimizationResults = null;
    }

    onMount(fetchAllData);
</script>

<svelte:window onkeydown={(e) => {
    if (e.key === 'Escape') {
        showExpenseModal = false;
        showAssetModal = false;
    }
}} />

<svelte:head>
    <title>Interactive Gantt Timeline — BudgetScript</title>
</svelte:head>

<div class="space-y-12">
    <!-- Header -->
    <header class="flex flex-col md:flex-row md:items-end justify-between gap-6">
        <div class="space-y-2">
            <h1 class="text-5xl font-black tracking-tight text-slate-900">
                Visual <span class="gradient-text">Timeline</span>.
            </h1>
            <p class="text-slate-500 font-medium text-lg">
                Drag-and-drop expenses to plan when they occur. View how parent assets and nested sub-assets grow over time.
            </p>
        </div>

        <!-- Scenario & Duration Selectors -->
        <div class="flex flex-wrap items-center gap-4 bg-white p-3 rounded-3xl shadow-sm border border-slate-100 dark:bg-slate-800 dark:border-slate-700">
            {#if scenarios.length > 0}
                <div class="flex items-center gap-2 pr-4 border-r border-slate-100 dark:border-slate-700 min-w-[220px]">
                    <SearchableDropdown
                        placeholder="Select Scenario..."
                        options={scenarios.map((s) => ({ id: s.id || "", label: s.name }))}
                        bind:value={selectedScenarioId}
                        onchange={handleScenarioChange}
                    />
                </div>

                <div class="flex items-center gap-2">
                    <span class="text-xs font-black uppercase text-slate-400">Timeline Length:</span>
                    <select
                        bind:value={projectionMonths}
                        onchange={runProjection}
                        class="bg-transparent font-bold text-slate-700 border-none outline-none pr-6 cursor-pointer dark:text-slate-200"
                    >
                        <option value={12}>12 Months</option>
                        <option value={24}>24 Months</option>
                        <option value={36}>36 Months</option>
                        <option value={60}>5 Years</option>
                        <option value={120}>10 Years</option>
                    </select>
                </div>
            {/if}
        </div>
    </header>

    {#if isLoading}
        <div class="glass-card p-20 flex flex-col items-center justify-center space-y-4">
            <Loader2 class="w-12 h-12 text-indigo-600 animate-spin" />
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Configuring Gantt Timeline workspace...
            </p>
        </div>
    {:else if error}
        <div class="glass-card p-12 text-center text-rose-600 space-y-4">
            <X class="w-12 h-12 mx-auto" />
            <p class="text-lg font-bold">{error}</p>
            <button onclick={fetchAllData} class="btn-primary inline-flex">Retry loading</button>
        </div>
    {:else if !activeScenario}
        <div class="glass-card p-12 text-center space-y-4">
            <p class="text-lg text-slate-500 font-medium">Please create a scenario first to view the timeline.</p>
            <a href="/scenarios" class="btn-primary inline-flex">Manage Scenarios</a>
        </div>
    {:else}
        <div class="bento-grid">
            <!-- Left Sidebar Controls -->
            <div class="md:col-span-4 space-y-8">
                <!-- Remainder Waterfall Card -->
                <div class="glass-card p-8 space-y-6">
                    <div class="flex items-center justify-between">
                        <div>
                            <h2 class="text-xl font-black uppercase tracking-tight text-slate-900">
                                Cash Waterfall
                            </h2>
                            <p class="text-xs text-slate-400 font-medium mt-1">
                                Order of investment and debt payoff for remainder funds.
                            </p>
                        </div>
                        <Layers class="w-5 h-5 text-indigo-500" />
                    </div>

                    <!-- Waterfall Ordering List -->
                    <div class="space-y-3">
                        {#if activeScenario.remainderOrder.length === 0}
                            <div class="p-6 border-2 border-dashed border-slate-100 rounded-2xl text-center dark:border-slate-700">
                                <p class="text-xs text-slate-400 font-medium">
                                    No assets or loans in remainder flow. All leftovers accrue in virtual accounts.
                                </p>
                            </div>
                        {:else}
                            {#each activeScenario.remainderOrder as entityId, index}
                                {@const asset = activeAssets.find(a => getID(a) === entityId)}
                                {@const loan = activeLoans.find(l => getID(l) === entityId)}
                                {@const entity = asset || loan}
                                {@const isAsset = !!asset}

                                {#if entity}
                                    <div
                                        class="flex items-center justify-between p-4 rounded-2xl border transition-all duration-200 cursor-grab active:cursor-grabbing shadow-sm
                                            {isAsset ? 'bg-cyan-50/40 border-cyan-100 text-cyan-900 dark:bg-cyan-950/20 dark:border-cyan-900/40 dark:text-cyan-200' : 'bg-purple-50/40 border-purple-100 text-purple-900 dark:bg-purple-950/20 dark:border-purple-900/40 dark:text-purple-200'}
                                            {dragOverWaterfallIndex === index ? 'border-dashed border-2 border-indigo-500 scale-[1.02]' : ''}"
                                        draggable="true"
                                        ondragstart={(e) => handleDragStartWaterfall(e, index)}
                                        ondragover={(e) => handleDragOverWaterfall(e, index)}
                                        ondrop={(e) => handleDropWaterfall(e, index)}
                                    >
                                        <div class="flex items-center gap-3">
                                            <GripVertical class="w-4 h-4 text-slate-400" />
                                            <div class="flex items-center gap-1.5">
                                                <span class="text-[10px] font-black w-5 h-5 rounded-full bg-white dark:bg-slate-800 flex items-center justify-center shadow-inner text-slate-500">
                                                    {index + 1}
                                                </span>
                                                <div class="flex flex-col">
                                                    <span class="text-xs font-bold truncate max-w-[120px]">{getName(entity)}</span>
                                                    <span class="text-[9px] font-semibold text-slate-400">
                                                        {isAsset ? 'Asset' : 'Loan'} &bull; {getInterestRate(entity)}%
                                                    </span>
                                                </div>
                                            </div>
                                        </div>

                                        <div class="flex items-center gap-3">
                                            <span class="text-xs font-black">{formatCurrency(getBalance(entity))}</span>
                                            <button
                                                onclick={() => removeFromWaterfall(entityId)}
                                                class="text-slate-400 hover:text-red-500 transition-colors p-1"
                                                title="Remove from waterfall"
                                            >
                                                <X class="w-4 h-4" />
                                            </button>
                                        </div>
                                    </div>
                                {/if}
                            {/each}
                        {/if}
                    </div>

                    <!-- Add Available Entities to Waterfall -->
                    {#if inactiveWaterfallItems.length > 0}
                        <div class="space-y-2 pt-2 border-t border-slate-100 dark:border-slate-800">
                            <span class="text-[9px] font-black uppercase tracking-wider text-slate-400 block mb-1">
                                Available Nodes (Click to Add):
                            </span>
                            <div class="flex flex-wrap gap-2">
                                {#each inactiveWaterfallItems as node}
                                    {@const isAsset = allAssets.some(a => getID(a) === getID(node))}
                                    <button
                                        onclick={() => addToWaterfall(getID(node))}
                                        class="px-3 py-1.5 text-[10px] font-bold rounded-xl border flex items-center gap-1 shadow-sm transition-all hover:scale-[1.02]
                                            {isAsset ? 'bg-white hover:bg-cyan-50 border-cyan-100 text-cyan-600 dark:bg-slate-800 dark:hover:bg-cyan-950/20 dark:border-cyan-900/40 dark:text-cyan-400' : 'bg-white hover:bg-purple-50 border-purple-100 text-purple-600 dark:bg-slate-800 dark:hover:bg-purple-950/20 dark:border-purple-900/40 dark:text-purple-400'}"
                                    >
                                        <Plus class="w-3 h-3" />
                                        {getName(node)} ({getInterestRate(node)}%)
                                    </button>
                                {/each}
                            </div>
                        </div>
                    {/if}
                </div>

                <!-- Auto Optimization suggestions Card -->
                <div class="glass-card p-8 space-y-6 bg-gradient-to-br from-indigo-500/5 via-purple-500/5 to-pink-500/5">
                    <div class="flex items-center justify-between">
                        <div class="space-y-1">
                            <h2 class="text-xl font-black uppercase tracking-tight text-slate-900 flex items-center gap-2">
                                Auto Optimizer <Sparkles class="w-4 h-4 text-amber-500" />
                            </h2>
                            <p class="text-xs text-slate-400 font-medium">
                                Suggest optimal orders for remainder allocations or flexible costs.
                            </p>
                        </div>
                    </div>

                    <!-- Optimizer selectors -->
                    <div class="grid grid-cols-2 gap-4">
                        <button
                            onclick={optimizeRemainderOrder}
                            disabled={isOptimizing || activeScenario.remainderOrder.length === 0}
                            class="px-4 py-3 bg-white dark:bg-slate-800 border hover:border-indigo-500 hover:text-indigo-600 rounded-2xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm flex flex-col items-center justify-center gap-2"
                        >
                            {#if isOptimizing}
                                <Loader2 class="w-4 h-4 animate-spin text-indigo-600" />
                                permuting...
                            {:else}
                                <Layers class="w-4 h-4 text-indigo-500" />
                                Waterfall
                            {/if}
                        </button>
                        <button
                            onclick={optimizeFlexibleExpenses}
                            disabled={isOptimizing || activeExpenses.filter(e => isFlexibleExpense(getName(e))).length === 0}
                            class="px-4 py-3 bg-white dark:bg-slate-800 border hover:border-indigo-500 hover:text-indigo-600 rounded-2xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm flex flex-col items-center justify-center gap-2"
                        >
                            {#if isOptimizing}
                                <Loader2 class="w-4 h-4 animate-spin text-indigo-600" />
                                shifting...
                            {:else}
                                <Calendar class="w-4 h-4 text-purple-500" />
                                Reschedule
                            {/if}
                        </button>
                    </div>

                    <!-- Waterfall Results -->
                    {#if optimizationResults}
                        <div class="space-y-4 border-t border-slate-100 dark:border-slate-800 pt-4" transition:slide>
                            <span class="text-[9px] font-black uppercase tracking-wider text-slate-400 block">Waterfall Outcomes:</span>
                            <div class="space-y-3">
                                {#each optimizationResults as res}
                                    <div class="p-4 bg-white/80 dark:bg-slate-800/80 border rounded-2xl shadow-sm space-y-2 relative overflow-hidden
                                        {res.isBest ? 'border-emerald-500 ring-2 ring-emerald-500/10' : 'border-slate-100 dark:border-slate-700'}">
                                        <div class="flex items-center justify-between">
                                            <span class="text-xs font-black text-slate-800 dark:text-slate-100">{res.name}</span>
                                            {#if res.isBest}
                                                <span class="bg-emerald-500 text-white text-[8px] font-black uppercase tracking-widest px-2 py-0.5 rounded-md">Best</span>
                                            {/if}
                                        </div>
                                        <p class="text-[9px] text-slate-400 font-medium leading-normal">{res.desc}</p>
                                        <div class="grid grid-cols-2 gap-2 text-[10px] pt-1">
                                            <div><span class="text-slate-400 font-medium">Net Worth:</span> <span class="font-bold">{formatCurrency(res.netWorth)}</span></div>
                                            <div><span class="text-slate-400 font-medium">Interest Cost:</span> <span class="font-bold">{formatCurrency(res.interestPaid)}</span></div>
                                        </div>
                                        <button
                                            onclick={() => applyRecommendation(res.order)}
                                            class="w-full mt-2 py-2 bg-slate-50 hover:bg-indigo-50 border border-slate-100 hover:border-indigo-500 hover:text-indigo-600 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all"
                                        >
                                            Apply Waterfall
                                        </button>
                                    </div>
                                {/each}
                            </div>
                        </div>
                    {/if}

                    <!-- Flexible Expenses Results -->
                    {#if expenseOptimizationResults}
                        <div class="space-y-4 border-t border-slate-100 dark:border-slate-800 pt-4" transition:slide>
                            <span class="text-[9px] font-black uppercase tracking-wider text-slate-400 block">Expense Placement Outcomes:</span>
                            <div class="space-y-3">
                                {#each expenseOptimizationResults as res}
                                    <div class="p-4 bg-white/80 dark:bg-slate-800/80 border rounded-2xl shadow-sm space-y-2 relative overflow-hidden
                                        {res.isBest ? 'border-emerald-500 ring-2 ring-emerald-500/10' : 'border-slate-100 dark:border-slate-700'}">
                                        <div class="flex items-center justify-between">
                                            <span class="text-xs font-black text-slate-800 dark:text-slate-100">{res.name}</span>
                                            {#if res.isBest}
                                                <span class="bg-emerald-500 text-white text-[8px] font-black uppercase tracking-widest px-2 py-0.5 rounded-md">Best</span>
                                            {/if}
                                        </div>
                                        <p class="text-[9px] text-slate-400 font-medium leading-normal">{res.desc}</p>
                                        <div class="grid grid-cols-2 gap-2 text-[10px] pt-1">
                                            <div><span class="text-slate-400 font-medium">Net Worth:</span> <span class="font-bold">{formatCurrency(res.netWorth)}</span></div>
                                            <div><span class="text-slate-400 font-medium">Interest Cost:</span> <span class="font-bold">{formatCurrency(res.interestPaid)}</span></div>
                                        </div>
                                        <button
                                            onclick={() => applyExpenseRescheduling(res.configs)}
                                            class="w-full mt-2 py-2 bg-slate-50 hover:bg-indigo-50 border border-slate-100 hover:border-indigo-500 hover:text-indigo-600 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all"
                                        >
                                            Apply Rescheduling
                                        </button>
                                    </div>
                                {/each}
                            </div>
                        </div>
                    {/if}
                </div>
            </div>

            <!-- Right timeline Area -->
            <div class="md:col-span-8 space-y-6">
                {#if isProjecting && months.length === 0}
                    <div class="glass-card p-24 flex flex-col items-center justify-center space-y-4">
                        <Loader2 class="w-10 h-10 text-indigo-600 animate-spin" />
                        <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[9px]">
                            Simulating budget periods...
                        </p>
                    </div>
                {:else if months.length === 0}
                    <div class="glass-card p-20 text-center text-slate-400 font-medium">
                        No projection months available. Click refresh or verify dates.
                    </div>
                {:else}
                    <!-- Sticky row headers + horizontally scrollable Gantt track -->
                    <div class="glass-card overflow-hidden flex border border-slate-100 dark:border-slate-800 shadow-xl" transition:fade>
                        
                        <!-- Sticky left headers column -->
                        <div class="w-64 border-r border-slate-100 dark:border-slate-800 bg-slate-50/50 dark:bg-slate-900/50 shrink-0 flex flex-col select-none">
                            <div class="h-24 border-b border-slate-100 dark:border-slate-800 flex items-center px-6 font-black text-[10px] uppercase tracking-wider text-slate-450 dark:text-slate-400">
                                Timeline Node Tracks
                            </div>
                            <div class="h-20 border-b border-slate-100 dark:border-slate-800 flex items-center px-6 font-black text-xs uppercase tracking-wider text-slate-400">
                                Variable Expenses
                            </div>

                            <button 
                                onclick={() => assetsCollapsed = !assetsCollapsed}
                                class="h-12 border-b border-slate-100 dark:border-slate-800 flex items-center justify-between px-6 font-black text-xs uppercase tracking-wider text-slate-400 hover:bg-slate-100/50 dark:hover:bg-slate-900/50 transition-colors cursor-pointer group shrink-0"
                            >
                                <span class="flex items-center gap-2">
                                    <Layers class="w-3.5 h-3.5" />
                                    Assets
                                </span>
                                {#if assetsCollapsed}
                                    <ChevronRight class="w-4 h-4 text-slate-300 group-hover:text-indigo-400" />
                                {:else}
                                    <ChevronDown class="w-4 h-4 text-slate-300 group-hover:text-indigo-400" />
                                {/if}
                            </button>

                            {#if !assetsCollapsed}
                                {#each activeAssets as asset}
                                    {@const hasLink = getActiveVersion(asset)?.dumpingLoanId || (getActiveVersion(asset)?.subAssets || []).some((sa: any) => sa.dumpingLoanId || sa.expenseId)}
                                    <div 
                                        onmouseenter={() => { hoveredEntityId = getID(asset); hoveredEntityType = "ASSET"; }}
                                        onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                        class="h-16 border-b border-slate-100 dark:border-slate-800 flex items-center justify-between px-6 bg-white dark:bg-slate-850 {getHighlightClasses(getID(asset), 'ASSET')}"
                                    >
                                        <div class="flex items-center gap-2 overflow-hidden">
                                            {#if getSubAssets(asset).length > 0}
                                                <button 
                                                    onclick={(e) => toggleAssetSubAssets(getID(asset), e)}
                                                    class="text-slate-300 hover:text-indigo-500 transition-colors p-0.5 shrink-0"
                                                >
                                                    {#if collapsedAssetIds.includes(getID(asset))}
                                                        <ChevronRight class="w-3.5 h-3.5" />
                                                    {:else}
                                                        <ChevronDown class="w-3.5 h-3.5" />
                                                    {/if}
                                                </button>
                                            {/if}
                                            <button
                                                onclick={() => openAssetDetails(asset)}
                                                class="text-xs font-black text-slate-800 dark:text-slate-200 hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors flex items-center gap-1.5 truncate max-w-[160px]"
                                            >
                                                <Layers class="w-3.5 h-3.5 text-indigo-500 shrink-0" />
                                                {#if hasLink}
                                                    <Link2 class="w-3.5 h-3.5 text-indigo-500 shrink-0" />
                                                {/if}
                                                <span class="truncate">{getName(asset)}</span>
                                            </button>
                                        </div>
                                        <button onclick={() => openAssetDetails(asset)} class="text-slate-350 hover:text-indigo-600 transition-colors shrink-0">
                                            <Edit3 class="w-3.5 h-3.5" />
                                        </button>
                                    </div>
                                    
                                    {#if !collapsedAssetIds.includes(getID(asset))}
                                        {#each getSubAssets(asset).filter((sa: any) => isEntityActive(sa.id || '', 'SUB_ASSET')) as sa}
                                            {@const saHasLink = sa.dumpingLoanId || sa.expenseId}
                                            <div 
                                                onmouseenter={() => { hoveredEntityId = sa.id; hoveredEntityType = "SUB_ASSET"; }}
                                                onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                                class="h-12 border-b border-slate-100 dark:border-slate-850 flex items-center px-6 pl-10 bg-slate-50/20 dark:bg-slate-900/10 {getHighlightClasses(sa.id, 'SUB_ASSET')}"
                                            >
                                                <button
                                                    onclick={() => openAssetDetails(asset)}
                                                    class="text-[11px] font-bold text-slate-400 hover:text-cyan-500 transition-colors flex items-center gap-1.5 truncate max-w-[180px] text-left"
                                                >
                                                    <Link2 class="w-3 h-3 text-cyan-400 shrink-0" />
                                                    {#if saHasLink}
                                                        <Link2 class="w-3 h-3 text-cyan-500 shrink-0" />
                                                    {/if}
                                                    <span class="truncate">{sa.name}</span>
                                                </button>
                                            </div>
                                        {/each}
                                    {/if}
                                {/each}
                            {/if}

                            <button 
                                onclick={() => loansCollapsed = !loansCollapsed}
                                class="h-12 border-b border-slate-100 dark:border-slate-800 flex items-center justify-between px-6 font-black text-xs uppercase tracking-wider text-slate-400 hover:bg-slate-100/50 dark:hover:bg-slate-900/50 transition-colors cursor-pointer group shrink-0"
                            >
                                <span class="flex items-center gap-2">
                                    <CreditCard class="w-3.5 h-3.5" />
                                    Loans
                                </span>
                                {#if loansCollapsed}
                                    <ChevronRight class="w-4 h-4 text-slate-300 group-hover:text-indigo-400" />
                                {:else}
                                    <ChevronDown class="w-4 h-4 text-slate-300 group-hover:text-indigo-400" />
                                {/if}
                            </button>

                            {#if !loansCollapsed}
                                {#each activeLoans as loan}
                                    {@const hasLink = activeAssets.some(a => getActiveVersion(a)?.dumpingLoanId === getID(loan) || getActiveVersion(a)?.subAssets?.some((sa: any) => sa.dumpingLoanId === getID(loan)))}
                                    <div 
                                        onmouseenter={() => { hoveredEntityId = getID(loan); hoveredEntityType = "LOAN"; }}
                                        onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                        class="h-16 border-b border-slate-100 dark:border-slate-800 flex items-center px-6 bg-white dark:bg-slate-850 {getHighlightClasses(getID(loan), 'LOAN')}"
                                    >
                                    <div class="text-xs font-black text-slate-800 dark:text-slate-200 flex items-center gap-1.5 truncate max-w-[180px]">
                                        <CreditCard class="w-3.5 h-3.5 text-rose-500 shrink-0" />
                                        {#if hasLink}
                                            <Link2 class="w-3.5 h-3.5 text-rose-500 shrink-0" />
                                        {/if}
                                        <span class="truncate">{getName(loan)}</span>
                                    </div>
                                </div>
                                {/each}
                            {/if}
                        </div>

                        <!-- Horizontally scrollable Gantt track -->
                        <div 
                            bind:this={timelineScrollContainer}
                            class="flex-1 overflow-x-auto custom-scrollbar bg-white dark:bg-slate-900"
                        >
                            <div class="relative flex flex-col min-w-max">
                                
                                <!-- Top Month Axis headers with Net Remainder -->
                                <div 
                                    onmousedown={handleTimelineMouseDown}
                                    onmouseleave={handleTimelineMouseLeave}
                                    onmouseup={handleTimelineMouseUp}
                                    onmousemove={handleTimelineMouseMove}
                                    role="presentation"
                                    class="h-24 border-b border-slate-100 dark:border-slate-800 flex bg-slate-50/30 dark:bg-slate-900/20 cursor-grab active:cursor-grabbing"
                                >
                                    {#each months as month}
                                        <div class="w-64 shrink-0 flex flex-col items-center justify-center border-r border-slate-100/50 dark:border-slate-800/50 relative px-4 select-none">
                                            <span class="text-[10px] font-black text-slate-400 uppercase tracking-widest leading-none">
                                                {new Date(month.date).toLocaleDateString("en-US", { month: "short" })}
                                            </span>
                                            <span class="text-xs text-slate-700 dark:text-slate-300 font-bold mt-1 leading-none">
                                                {new Date(month.date).getFullYear()}
                                            </span>
                                            
                                            <!-- Prominent Net Remainder Badge -->
                                            <div class="mt-2.5 px-3 py-1 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20 rounded-full font-black text-[10px] flex items-center gap-1">
                                                <span class="text-[8px] font-black uppercase text-emerald-500/70">Rem:</span>
                                                {formatCurrency(month.remainder)}
                                            </div>
                                        </div>
                                    {/each}
                                </div>

                                <!-- Expenses row track -->
                                <div class="h-20 border-b border-slate-100 dark:border-slate-800 flex bg-slate-50/10 dark:bg-slate-900/10">
                                    {#each months as month}
                                        {@const monthKey = getMonthKey(month.date)}
                                        {@const isOver = activeDragOverMonthStr === monthKey}
                                        <div
                                            class="w-64 shrink-0 border-r border-slate-100/50 dark:border-slate-800/50 relative flex flex-col justify-center p-1.5 gap-1 transition-colors duration-205 group
                                                {isOver ? 'bg-indigo-50/20 border-2 border-indigo-500 z-10' : ''}"
                                            ondragover={(e) => handleDragOverMonth(e, monthKey)}
                                            ondragenter={(e) => handleDragOverMonth(e, monthKey)}
                                            ondragleave={handleDragLeaveMonth}
                                            ondrop={(e) => handleDropExpense(e, monthKey)}
                                        >
                                            <!-- Small + sign button in top right of cell, visible on hover -->
                                            <button
                                                onclick={() => openNewExpenseModal(month.date)}
                                                class="absolute top-1 right-1 opacity-0 group-hover:opacity-100 focus:opacity-100 p-1 rounded bg-slate-50 hover:bg-indigo-650 hover:text-white dark:bg-slate-850 dark:hover:bg-indigo-600 transition-all text-slate-400 z-20"
                                                title="Add Expense"
                                            >
                                                <Plus class="w-3 h-3" />
                                            </button>
                                            {#if month.breakdown?.expenses}
                                                {#each month.breakdown.expenses as exp}
                                                    {@const expObj = activeExpenses.find(e => getName(e) === exp.entityName)}
                                                    {#if expObj}
                                                        {@const isFlexible = isFlexibleExpense(exp.name)}
                                                        {@const expHasLink = allAssets.some(a => (getActiveVersion(a)?.subAssets || []).some((sa: any) => sa.expenseId === getID(expObj)))}
                                                        <button
                                                            onclick={() => openExpenseDetails(exp, expObj)}
                                                            onmouseenter={() => { hoveredEntityId = getID(expObj); hoveredEntityType = "EXPENSE"; }}
                                                            onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                                            draggable="true"
                                                            ondragstart={(e) => handleDragStartExpense(e, expObj || { id: exp.entityName, name: exp.entityName, activeVersion: { amount: exp.amount, dueDate: month.date, slices: [] } })}
                                                            class="px-2 py-1 border rounded-lg shadow-sm flex items-center justify-between text-left transition-all hover:scale-102 w-full {getHighlightClasses(getID(expObj), 'EXPENSE')}
                                                                {isFlexible 
                                                                    ? 'bg-amber-50/90 hover:bg-amber-100 border-amber-200 text-amber-900 dark:bg-amber-950/40 dark:border-amber-900/40 dark:text-amber-200 cursor-grab active:cursor-grabbing' 
                                                                    : 'bg-rose-50/90 hover:bg-rose-100 border-rose-200 text-rose-900 dark:bg-rose-950/40 dark:border-rose-900/40 dark:text-rose-200 cursor-not-allowed'}"
                                                        >
                                                            <span class="font-black text-[9px] truncate flex items-center gap-1 max-w-[120px]">
                                                                {#if expHasLink}
                                                                    <Link2 class="w-3 h-3 text-indigo-500 shrink-0" />
                                                                {/if}
                                                                <span class="truncate">{cleanExpenseName(exp.name)}</span>
                                                                {#if getActiveVersion(expObj)?.subExpenses && getActiveVersion(expObj).subExpenses.length > 0}
                                                                    <span class="px-1 py-0.5 bg-indigo-100 text-indigo-800 dark:bg-indigo-900/50 dark:text-indigo-300 text-[7px] rounded font-bold font-mono uppercase tracking-tight shrink-0" title={getActiveVersion(expObj).subExpenses.map((s: any) => `${s.description}: ${formatCurrency(s.amount)}`).join('\n')}>
                                                                        {getActiveVersion(expObj).subExpenses.length} items
                                                                    </span>
                                                                {/if}
                                                            </span>
                                                            <div class="flex items-center gap-1 text-[9px] font-black shrink-0">
                                                                <span>{formatCurrency(exp.amount)}</span>
                                                                {#if !isFlexible}
                                                                    <Lock class="w-3 h-3 text-rose-500 shrink-0" />
                                                                {:else}
                                                                    <Unlock class="w-3 h-3 text-amber-500 shrink-0" />
                                                                {/if}
                                                            </div>
                                                        </button>
                                                    {/if}
                                                {/each}
                                            {/if}
                                        </div>
                                    {/each}
                                </div>

                                <!-- Assets & Sub-assets horizontal bars mapping -->
                                <div class="h-12 border-b border-slate-100 dark:border-slate-800 flex bg-slate-50/50 dark:bg-slate-900/50 relative">
                                    {#each months as month}
                                        <div class="w-64 shrink-0 border-r border-slate-100/30 dark:border-slate-800/30 h-full"></div>
                                    {/each}
                                </div>
                                
                                {#if !assetsCollapsed}
                                    {#each activeAssets as asset}
                                        {@const actualAssetEnd = getActualEndIndex(asset)}
                                        {@const span = getAssetColSpan(getActiveVersion(asset)?.startDate || "", getActiveVersion(asset)?.endDate || "", actualAssetEnd)}
                                        <!-- Asset row -->
                                        <div class="h-16 border-b border-slate-100 dark:border-slate-800 flex bg-white dark:bg-slate-900 relative">
                                            {#each months as month}
                                                <div class="w-64 shrink-0 border-r border-slate-100/30 dark:border-slate-800/30 h-full"></div>
                                            {/each}
                                            
                                            {#if span.visible}
                                                {@const hasLink = getActiveVersion(asset)?.dumpingLoanId || (getActiveVersion(asset)?.subAssets || []).some((sa: any) => sa.dumpingLoanId || sa.expenseId)}
                                                <button
                                                    onclick={() => openAssetDetails(asset)}
                                                    onmouseenter={() => { hoveredEntityId = getID(asset); hoveredEntityType = "ASSET"; }}
                                                    onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                                    class="absolute top-3 bottom-3 rounded-2xl bg-gradient-to-r from-indigo-500/80 via-purple-500/80 to-pink-500/80 text-white font-black text-xs px-4 flex items-center justify-between shadow-md hover:scale-[1.01] active:scale-[0.99] transition-all {getHighlightClasses(getID(asset), 'ASSET')}"
                                                    style="left: {span.startIdx * 256 + 8}px; width: {(span.endIdx - span.startIdx + 1) * 256 - 16}px;"
                                                >
                                                    <span class="truncate flex items-center gap-1.5">
                                                        {#if hasLink}
                                                            <Link2 class="w-3.5 h-3.5 text-white/90 shrink-0" />
                                                        {/if}
                                                        <span class="truncate">{getName(asset)} ({getInterestRate(asset)}%)</span>
                                                    </span>
                                                    <span class="text-[10px] font-black bg-white/20 px-2.5 py-0.5 rounded-full select-none">
                                                        {formatCurrency(getBalance(asset))}
                                                    </span>
                                                </button>
                                            {/if}
                                        </div>

                                        <!-- Sub-asset rows -->
                                        {#if !collapsedAssetIds.includes(getID(asset))}
                                            {#each getSubAssets(asset).filter((sa: any) => isEntityActive(sa.id || '', 'SUB_ASSET')) as sa}
                                                {@const actualSubAssetEnd = getActualEndIndex(asset, sa)}
                                                {@const saSpan = getAssetColSpan(sa.startDate, sa.endDate, actualSubAssetEnd)}
                                                <div class="h-12 border-b border-slate-100 dark:border-slate-850 flex bg-slate-50/10 dark:bg-slate-900/10 relative">
                                                    {#each months as month}
                                                        <div class="w-64 shrink-0 border-r border-slate-100/30 dark:border-slate-800/30 h-full"></div>
                                                    {/each}
                                                    
                                                    {#if saSpan.visible}
                                                        {@const isThisResizing = isResizingSubAsset && resizingSubAssetData?.sa.id === sa.id}
                                                        {@const displayStartIdx = (isThisResizing && resizingSubAssetData) ? resizingSubAssetData.currentStartIdx : saSpan.startIdx}
                                                        {@const displayEndIdx = (isThisResizing && resizingSubAssetData) ? resizingSubAssetData.currentEndIdx : saSpan.endIdx}
                                                        {@const saHasLink = sa.dumpingLoanId || sa.expenseId}
                                                        <button
                                                            onclick={() => openAssetDetails(asset)}
                                                            onmouseenter={() => { hoveredEntityId = sa.id; hoveredEntityType = "SUB_ASSET"; }}
                                                            onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                                            class="absolute top-2.5 bottom-2.5 rounded-xl bg-gradient-to-r from-teal-500/60 to-cyan-500/60 text-white font-bold text-[10px] px-3 flex items-center justify-between shadow-sm border border-white/10 hover:scale-[1.01] active:scale-[0.99] transition-all text-left {getHighlightClasses(sa.id, 'SUB_ASSET')} {isThisResizing ? 'z-50 ring-2 ring-cyan-400 scale-[1.02]' : ''}"
                                                            style="left: {displayStartIdx * 256 + 16}px; width: {(displayEndIdx - displayStartIdx + 1) * 256 - 32}px;"
                                                        >
                                                            <!-- Resize Handles -->
                                                            <div 
                                                                class="absolute left-0 top-0 bottom-0 w-4 cursor-col-resize z-10 group/handle flex items-center justify-center"
                                                                onmousedown={(e) => startResizingSubAsset(e, asset, sa, 'start')}
                                                            >
                                                                <div class="w-1.5 h-4 bg-white/30 rounded-full group-hover/handle:bg-white/60 transition-colors"></div>
                                                            </div>
                                                            
                                                            <div 
                                                                class="absolute right-0 top-0 bottom-0 w-4 cursor-col-resize z-10 group/handle flex items-center justify-center"
                                                                onmousedown={(e) => startResizingSubAsset(e, asset, sa, 'end')}
                                                            >
                                                                <div class="w-1.5 h-4 bg-white/30 rounded-full group-hover/handle:bg-white/60 transition-colors"></div>
                                                            </div>

                                                            <span class="truncate flex items-center gap-1 pl-1.5">
                                                                {#if saHasLink}
                                                                    <Link2 class="w-3 h-3 text-white/90 shrink-0" />
                                                                {/if}
                                                                <span class="truncate">{sa.name}</span>
                                                            </span>
                                                            <span class="font-black font-sans pr-1.5">Target: {formatCurrency(sa.targetValue)}</span>
                                                        </button>
                                                    {/if}
                                                </div>
                                            {/each}
                                        {/if}
                                    {/each}
                                {/if}

                                <!-- Loans horizontal bars mapping -->
                                <div class="h-12 border-b border-slate-100 dark:border-slate-800 flex bg-slate-50/50 dark:bg-slate-900/50 relative">
                                    {#each months as month}
                                        <div class="w-64 shrink-0 border-r border-slate-100/30 dark:border-slate-800/30 h-full"></div>
                                    {/each}
                                </div>

                                {#if !loansCollapsed}
                                    {#each activeLoans as loan}
                                        {@const actualLoanEnd = getLoanActualEndIndex(loan)}
                                        {@const span = getLoanColSpan(loan, actualLoanEnd)}
                                        <!-- Loan row -->
                                        <div class="h-16 border-b border-slate-100 dark:border-slate-800 flex bg-white dark:bg-slate-900 relative">
                                            {#each months as month}
                                                <div class="w-64 shrink-0 border-r border-slate-100/30 dark:border-slate-800/30 h-full"></div>
                                            {/each}
                                            
                                            {#if span.visible}
                                                {@const hasLink = activeAssets.some(a => getActiveVersion(a)?.dumpingLoanId === getID(loan) || getActiveVersion(a)?.subAssets?.some((sa: any) => sa.dumpingLoanId === getID(loan)))}
                                                <div
                                                    onmouseenter={() => { hoveredEntityId = getID(loan); hoveredEntityType = "LOAN"; }}
                                                    onmouseleave={() => { hoveredEntityId = null; hoveredEntityType = null; }}
                                                    class="absolute top-3 bottom-3 rounded-2xl bg-gradient-to-r from-rose-500/80 via-pink-500/80 to-purple-500/80 text-white font-black text-xs px-4 flex items-center justify-between shadow-md select-none {getHighlightClasses(getID(loan), 'LOAN')}"
                                                    style="left: {span.startIdx * 256 + 8}px; width: {(span.endIdx - span.startIdx + 1) * 256 - 16}px;"
                                                >
                                                    <span class="truncate flex items-center gap-1.5">
                                                        {#if hasLink}
                                                            <Link2 class="w-3.5 h-3.5 text-white/90 shrink-0" />
                                                        {/if}
                                                        <span class="truncate">{getName(loan)} ({getInterestRate(loan)}%)</span>
                                                    </span>
                                                    <span class="text-[10px] font-black bg-white/20 px-2.5 py-0.5 rounded-full">
                                                        {formatCurrency(getBalance(loan))}
                                                    </span>
                                                </div>
                                            {/if}
                                        </div>
                                    {/each}
                                {/if}

                            </div>
                        </div>
                    </div>
                {/if}
            </div>
        </div>
    {/if}
</div>
<ExpenseDetailModal
    bind:open={showExpenseModal}
    bind:selectedExpenseObj
    months={months}
    activeAssets={activeAssets}
    bind:isProjecting
    onSave={saveExpenseChanges}
    onCreateFundingSubAsset={async (assetId, remainderConsumer) => {
        fundingAssetId = assetId;
        fundingRemainderConsumer = remainderConsumer;
        await createFundingPlan();
    }}
    formatCurrency={formatCurrency}
    parseMonthYear={parseMonthYear}
    getMonthKey={getMonthKey}
    getMonthsBetween={getMonthsBetween}
    isFlexibleExpense={isFlexibleExpense}
    cleanExpenseName={cleanExpenseName}
    updateExpenseDetails={updateExpenseDetails}
/>

<AssetDetailModal
    bind:open={showAssetModal}
    bind:editingAssetObj
    simulatedYields={simulatedYields}
    bind:assetSaveError={assetSaveError}
    onSave={saveAssetChanges}
    toInputMonth={toInputMonth}
    fromInputMonth={fromInputMonth}
/>

<style>
    /* Styling for custom horizontal scrollbars in sticky Gantt view */
    .custom-scrollbar::-webkit-scrollbar {
        height: 8px;
    }
    .custom-scrollbar::-webkit-scrollbar-track {
        background: #f1f5f9;
        border-radius: 999px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #cbd5e1;
        border-radius: 999px;
        border: 2px solid #f1f5f9;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #94a3b8;
    }

    :global(.dark) .custom-scrollbar::-webkit-scrollbar-track {
        background: #0f172a;
    }
    :global(.dark) .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #334155;
        border-color: #0f172a;
    }
</style>
