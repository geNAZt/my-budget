<script lang="ts">
    import { onMount } from "svelte";
    import { Line, Bar, Doughnut } from "svelte-chartjs";
    import {
        Chart as ChartJS,
        Filler,
        LineElement,
        PointElement,
        LinearScale,
        CategoryScale,
        Title,
        Tooltip,
        Legend,
        BarElement,
        ArcElement,
    } from "chart.js";
    import {
        TrendingUp,
        Calendar,
        Layers,
        Activity,
        Sparkles,
        Loader2,
        CheckCircle2,
        HelpCircle,
        HandCoins,
        Wallet,
        PieChart,
        ChevronRight,
        Download,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        ScenarioListSchema,
        AssetListSchema,
        ScenarioSchema,
        ProjectionMonthSchema,
        YieldMapSchema,
        PerformanceMetricsSchema,
        PenaltyAnalysisSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";

    interface PenaltyEvent {
        type: string;
        date: string;
        assetName: string;
        lotId: string;
        lotCreatedAt: string;
        amount: number;
        principalSold: number;
        penaltyPaid: number;
        monthsHeld: number;
        interestGenerated: number;
    }

    const PALETTE = [
        {
            border: "#6366f1",
            fill: "rgba(99, 102, 241, 0.04)",
            bgClass: "bg-indigo-50 text-indigo-600 border-indigo-200",
        },
        {
            border: "#a855f7",
            fill: "rgba(168, 85, 247, 0.04)",
            bgClass: "bg-purple-50 text-purple-600 border-purple-200",
        },
        {
            border: "#10b981",
            fill: "rgba(16, 185, 129, 0.04)",
            bgClass: "bg-emerald-50 text-emerald-600 border-emerald-200",
        },
        {
            border: "#f59e0b",
            fill: "rgba(245, 158, 11, 0.04)",
            bgClass: "bg-amber-50 text-amber-600 border-amber-200",
        },
        {
            border: "#f43f5e",
            fill: "rgba(244, 63, 94, 0.04)",
            bgClass: "bg-rose-50 text-rose-600 border-rose-200",
        },
        {
            border: "#64748b",
            fill: "rgba(100, 116, 139, 0.04)",
            bgClass: "bg-slate-100 text-slate-600 border-slate-300",
        },
    ];

    interface Scenario {
        id: string;
        name: string;
        description: string;
        projectionMonths: number;
        passiveIncomePercentage: number;
        startDate?: string;
    }

    let scenarios = $state<Scenario[]>([]);
    let selectedScenarioIds = $state<string[]>([]);
    let activeMetric = $state<"net_worth" | "assets" | "cash" | "debt">(
        "net_worth",
    );
    let timeHorizonYears = $state<number>(30);
    let allAssets = $state<any[]>([]);

    let projections = $state<Record<string, any>>({});
    let loadingProjections = $state<Record<string, boolean>>({});
    let isInitialLoading = $state(true);

    const isSomeScenarioLoading = $derived(
        selectedScenarioIds.some((id) => loadingProjections[id]),
    );

    async function fetchScenarios() {
        try {
            const [resp, err] = await wsCall("scenarios::list", null, null, [
                ScenarioListSchema,
            ]).one();
            if (err) throw err;
            scenarios = resp ? resp.scenarios : [];

            // Default: Select first two scenarios if available for instant comparison
            if (scenarios.length > 0) {
                selectedScenarioIds = [scenarios[0].id];
                if (scenarios.length > 1) {
                    selectedScenarioIds = [scenarios[0].id, scenarios[1].id];
                }
            }
        } catch (err: any) {
            console.error(err);
        } finally {
            isInitialLoading = false;
        }
    }

    async function fetchAssets() {
        try {
            const [resp, err] = await wsCall("assets::list", null, null, [
                AssetListSchema,
            ]).one();
            if (err) throw err;
            allAssets = resp ? resp.assets : [];
        } catch (err: any) {
            console.error(err);
        }
    }

    async function fetchProjection(id: string) {
        if (loadingProjections[id]) return;
        loadingProjections[id] = true;

        projections[id] = {
            scenario_name: scenarios.find((s) => s.id === id)?.name || "",
            simulated_yields: {},
            months: [],
            penalty_events: [],
        };

        try {
            const callResult = wsCall(
                "scenarios::projection",
                ScenarioSchema,
                { id },
                [
                    ProjectionMonthSchema,
                    YieldMapSchema,
                    PerformanceMetricsSchema,
                    PenaltyAnalysisSchema,
                    ErrorSchema,
                ],
            );

            await new Promise<void>(async (resolve, reject) => {
                try {
                    for await (const [message, error] of callResult.many()) {
                        if (error) {
                            reject(error);
                            break;
                        }
                        if (message) {
                            const typeName = (message as any).$typeName;
                            if (typeName === "api.YieldMap") {
                                projections[id].simulated_yields = {
                                    ...((message as any).yields || {}),
                                };
                            } else if (typeName === "api.ProjectionMonth") {
                                projections[id].months = [
                                    ...projections[id].months,
                                    message,
                                ];
                            } else if (typeName === "api.PenaltyAnalysis") {
                                projections[id].penalty_events = [
                                    ...((message as any).events || []),
                                ];
                            }
                        }
                    }
                    resolve();
                } catch (e) {
                    reject(e);
                }
            });
        } catch (err: any) {
            console.error(err);
        } finally {
            loadingProjections[id] = false;
        }
    }

    function toggleScenarioSelection(id: string) {
        if (selectedScenarioIds.includes(id)) {
            // Keep at least one selected scenario
            if (selectedScenarioIds.length > 1) {
                selectedScenarioIds = selectedScenarioIds.filter(
                    (x) => x !== id,
                );
            }
        } else {
            // Restrict overlay to max palette size
            if (selectedScenarioIds.length < PALETTE.length) {
                selectedScenarioIds = [...selectedScenarioIds, id];
            } else {
                alert(
                    `You can overlay up to ${PALETTE.length} scenarios at the same time.`,
                );
            }
        }
    }

    // Reactively fetch projections for selected scenarios
    $effect(() => {
        for (const id of selectedScenarioIds) {
            if (!projections[id] && !loadingProjections[id]) {
                fetchProjection(id);
            }
        }
    });

    onMount(async () => {
        ChartJS.register(
            LineElement,
            PointElement,
            LinearScale,
            CategoryScale,
            Title,
            Tooltip,
            Legend,
            Filler,
            BarElement,
            ArcElement,
        );
        await fetchScenarios();
        await fetchAssets();
    });

    // Reactive choosing of scenario and asset for detailed explorer
    let selectedAssetScenarioId = $state<string>("");
    let selectedAssetName = $state<string>("");

    function exportTaxAnalysisCSV(sid: string) {
        const events = projections[sid]?.penalty_events || [];
        if (events.length === 0) return;

        const headers = [
            "Date",
            "Asset",
            "Lot ID",
            "Created",
            "Type",
            "Amount",
            "Principal Sold",
            "Penalty/Tax",
            "Interest Generated",
            "Months Held",
            "Net Impact",
        ];

        const rows = events.map((e) => [
            new Date(e.date).toLocaleDateString("de-DE", {
                month: "2-digit",
                year: "numeric",
            }),
            e.assetName,
            e.lotId,
            new Date(e.lotCreatedAt).toLocaleDateString("de-DE", {
                month: "2-digit",
                year: "numeric",
            }),
            e.type,
            e.amount.toFixed(2),
            e.principalSold.toFixed(2),
            e.penaltyPaid.toFixed(2),
            e.interestGenerated.toFixed(2),
            e.monthsHeld,
            (e.amount - e.penaltyPaid).toFixed(2),
        ]);

        const csvContent = [
            headers.join(";"),
            ...rows.map((r) => r.join(";")),
        ].join("\n");

        const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
        const link = document.createElement("a");
        const url = URL.createObjectURL(blob);
        link.setAttribute("href", url);
        link.setAttribute(
            "download",
            `tax_analysis_${projections[sid].scenario_name.replace(/\s+/g, "_")}.csv`,
        );
        link.style.visibility = "hidden";
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }

    let selectedAssetInfo = $derived.by(() => {
        return allAssets.find((a) => a.name === selectedAssetName) || null;
    });

    // Reactively ensure selectedAssetScenarioId is one of the active scenarios
    $effect(() => {
        if (
            selectedScenarioIds.length > 0 &&
            !selectedScenarioIds.includes(selectedAssetScenarioId)
        ) {
            selectedAssetScenarioId = selectedScenarioIds[0];
        }
    });

    // Extract unique asset names from the selected scenario projection
    let availableAssets = $derived.by(() => {
        if (!selectedAssetScenarioId) return [];
        const proj = projections[selectedAssetScenarioId];
        if (!proj || !proj.months) return [];

        const assets = new Set<string>();
        for (const m of proj.months) {
            if (m.breakdown && m.breakdown.assets) {
                for (const entry of m.breakdown.assets) {
                    if (entry.entityName) {
                        assets.add(entry.entityName);
                    }
                }
            }
        }
        return Array.from(assets).sort();
    });

    // Reactively select the first asset if the current selection is no longer valid
    $effect(() => {
        if (availableAssets.length > 0) {
            if (!availableAssets.includes(selectedAssetName)) {
                selectedAssetName = availableAssets[0];
            }
        } else {
            selectedAssetName = "";
        }
    });

    let assetChartLabels = $derived.by(() => {
        if (!selectedAssetScenarioId) return [];
        const proj = projections[selectedAssetScenarioId];
        if (!proj || !proj.months) return [];

        const limitMonths = Math.min(proj.months.length, timeHorizonYears * 12);
        return proj.months.slice(0, limitMonths).map((m: any) => {
            const d = new Date(m.date);
            return d.toLocaleDateString("de-DE", {
                year: "2-digit",
                month: "2-digit",
            });
        });
    });

    let assetChartData = $derived.by(() => {
        if (!selectedAssetScenarioId || !selectedAssetName) return null;
        const proj = projections[selectedAssetScenarioId];
        if (!proj || !proj.months) return null;

        const limitMonths = Math.min(proj.months.length, timeHorizonYears * 12);
        const activeMonths = proj.months.slice(0, limitMonths);

        const balances: number[] = [];
        const contributions: number[] = [];
        const interests: number[] = [];
        const penalties: number[] = [];
        const withdrawals: number[] = [];

        let lastBalance = 0;
        let cumulativeContributions = 0;
        let cumulativeInterest = 0;
        let cumulativePenalty = 0;
        let cumulativeWithdrawals = 0;

        activeMonths.forEach((m: any) => {
            const entries = (m.breakdown?.assets || []).filter(
                (e: any) => e.entityName === selectedAssetName,
            );

            let monthlyContribution = 0;
            let monthlyInterest = 0;
            let monthlyPenalty = 0;
            let monthlyWithdrawal = 0;
            let currentBalance = lastBalance;

            entries.forEach((e: any) => {
                if (e.amount > 0) {
                    monthlyContribution += e.amount;
                } else if (e.amount < 0) {
                    monthlyWithdrawal += Math.abs(e.amount);
                }
                monthlyInterest += e.interest || 0;
                monthlyPenalty += e.penalty || 0;
                if (e.balance !== undefined && e.balance > 0) {
                    currentBalance = e.balance;
                }
            });

            cumulativeContributions += monthlyContribution;
            cumulativeInterest += monthlyInterest;
            cumulativePenalty += monthlyPenalty;
            cumulativeWithdrawals += monthlyWithdrawal;

            balances.push(currentBalance);
            contributions.push(cumulativeContributions);
            interests.push(cumulativeInterest);
            penalties.push(cumulativePenalty);
            withdrawals.push(cumulativeWithdrawals);

            lastBalance = currentBalance;
        });

        return {
            labels: assetChartLabels,
            datasets: [
                {
                    label: "Balance (Left Axis)",
                    data: balances,
                    borderColor: "#10b981", // emerald
                    backgroundColor: "rgba(16, 185, 129, 0.03)",
                    borderWidth: 3,
                    pointRadius: 1,
                    pointHoverRadius: 6,
                    tension: 0.25,
                    fill: true,
                    yAxisID: "y_balance",
                },
                {
                    label: "Total Contributions (Right Axis)",
                    data: contributions,
                    borderColor: "#6366f1", // indigo
                    backgroundColor: "transparent",
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4,
                    tension: 0.25,
                    yAxisID: "y_flows",
                },
                {
                    label: "Total Interest / Growth (Right Axis)",
                    data: interests,
                    borderColor: "#f59e0b", // amber
                    backgroundColor: "transparent",
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4,
                    tension: 0.25,
                    yAxisID: "y_flows",
                },
                {
                    label: "Total Withdrawals (Right Axis)",
                    data: withdrawals,
                    borderColor: "#06b6d4", // cyan
                    backgroundColor: "transparent",
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4,
                    tension: 0.25,
                    yAxisID: "y_flows",
                },
                {
                    label: "Total Penalty Paid (Right Axis)",
                    data: penalties,
                    borderColor: "#f43f5e", // rose
                    backgroundColor: "transparent",
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4,
                    tension: 0.25,
                    yAxisID: "y_flows",
                },
            ],
        };
    });

    const assetChartOptions = {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: true,
                position: "top" as const,
                labels: {
                    boxWidth: 10,
                    font: {
                        size: 10,
                        weight: "bold" as const,
                        family: "system-ui",
                    },
                    color: "#64748b",
                },
            },
            tooltip: {
                backgroundColor: "rgba(15, 23, 42, 0.95)",
                titleFont: {
                    size: 12,
                    weight: "bold" as const,
                    family: "system-ui",
                },
                bodyFont: { size: 12, family: "system-ui" },
                padding: 12,
                cornerRadius: 12,
                displayColors: true,
                callbacks: {
                    label: function (context: any) {
                        let label = context.dataset.label || "";
                        label = label
                            .replace(" (Left Axis)", "")
                            .replace(" (Right Axis)", "");
                        if (label) {
                            label += ": ";
                        }
                        if (context.parsed.y !== null) {
                            label +=
                                "€ " +
                                context.parsed.y.toLocaleString("de-DE", {
                                    minimumFractionDigits: 2,
                                    maximumFractionDigits: 2,
                                });
                        }
                        return label;
                    },
                },
            },
        },
        scales: {
            x: {
                grid: {
                    display: false,
                },
                ticks: {
                    color: "#94a3b8",
                    font: { size: 10, weight: "bold" as const },
                    maxTicksLimit: 12,
                },
            },
            y_balance: {
                type: "linear" as const,
                position: "left" as const,
                grid: {
                    color: "#f1f5f9",
                },
                ticks: {
                    color: "#10b981",
                    font: { size: 10, weight: "bold" as const },
                    callback: function (value: any) {
                        if (value >= 1e6)
                            return "€" + (value / 1e6).toFixed(1) + "M";
                        if (value >= 1e3)
                            return "€" + (value / 1e3).toFixed(0) + "k";
                        return "€" + value;
                    },
                },
                title: {
                    display: true,
                    text: "Total Balance",
                    color: "#10b981",
                    font: {
                        size: 10,
                        weight: "black" as const,
                        family: "system-ui",
                    },
                },
            },
            y_flows: {
                type: "linear" as const,
                position: "right" as const,
                grid: {
                    drawOnChartArea: false,
                },
                ticks: {
                    color: "#6366f1",
                    font: { size: 10, weight: "bold" as const },
                    callback: function (value: any) {
                        if (value >= 1e6)
                            return "€" + (value / 1e6).toFixed(1) + "M";
                        if (value >= 1e3)
                            return "€" + (value / 1e3).toFixed(0) + "k";
                        return "€" + value;
                    },
                },
                title: {
                    display: true,
                    text: "Accumulated Flows/Growth",
                    color: "#6366f1",
                    font: {
                        size: 10,
                        weight: "black" as const,
                        family: "system-ui",
                    },
                },
            },
        },
    };

    let assetSummary = $derived.by(() => {
        if (!assetChartData || !assetChartData.datasets) return null;

        const balances = assetChartData.datasets[0].data as number[];
        const contributions = assetChartData.datasets[1].data as number[];
        const interests = assetChartData.datasets[2].data as number[];
        const withdrawals = assetChartData.datasets[3].data as number[];
        const penalties = assetChartData.datasets[4].data as number[];

        const endingBalance =
            balances.length > 0 ? balances[balances.length - 1] : 0;
        const totalContributions =
            contributions.length > 0
                ? contributions[contributions.length - 1]
                : 0;
        const totalInterest =
            interests.length > 0 ? interests[interests.length - 1] : 0;
        const totalWithdrawals =
            withdrawals.length > 0 ? withdrawals[withdrawals.length - 1] : 0;
        const totalPenalties =
            penalties.length > 0 ? penalties[penalties.length - 1] : 0;

        return {
            endingBalance,
            totalContributions,
            totalInterest,
            totalWithdrawals,
            totalPenalties,
        };
    });

    let finalRealSplit = $derived.by(() => {
        if (!selectedAssetScenarioId || !selectedAssetName) return null;
        const proj = projections[selectedAssetScenarioId];
        if (!proj || !proj.months) return null;

        const limitMonths = Math.min(proj.months.length, timeHorizonYears * 12);
        const activeMonths = proj.months.slice(0, limitMonths);

        // Scan backwards to find the last month with real splits for this asset
        for (let i = activeMonths.length - 1; i >= 0; i--) {
            const m = activeMonths[i];
            const entries = (m.breakdown?.assets || []).filter(
                (e: any) => e.entityName === selectedAssetName,
            );
            for (const e of entries) {
                if (e.realSplit && Object.keys(e.realSplit).length > 0) {
                    return e.realSplit;
                }
            }
        }
        return null;
    });

    let trackerCumulativeFlows = $derived.by(() => {
        if (!selectedAssetScenarioId || !selectedAssetName)
            return {} as Record<string, number>;
        const proj = projections[selectedAssetScenarioId];
        if (!proj || !proj.months) return {} as Record<string, number>;

        const limitMonths = Math.min(proj.months.length, timeHorizonYears * 12);
        const activeMonths = proj.months.slice(0, limitMonths);

        const totals: Record<string, number> = {};
        activeMonths.forEach((m: any) => {
            const entries = (m.breakdown?.assets || []).filter(
                (e: any) => e.entityName === selectedAssetName,
            );
            entries.forEach((e: any) => {
                if (e.trackerFlows) {
                    for (const [tracker, amount] of Object.entries(
                        e.trackerFlows,
                    )) {
                        totals[tracker] =
                            (totals[tracker] || 0) + (amount as number);
                    }
                }
            });
        });
        return totals;
    });

    // Dynamic Chart Labels & Datasets
    let chartLabels = $derived.by(() => {
        const activeProjections = selectedScenarioIds
            .map((id) => projections[id])
            .filter((p) => p && p.months && p.months.length > 0);

        if (activeProjections.length === 0) return [];

        // Determine chronological X-Axis aligned to the longest dataset
        activeProjections.sort((a, b) => b.months.length - a.months.length);
        const longest = activeProjections[0];
        const limitMonths = Math.min(
            longest.months.length,
            timeHorizonYears * 12,
        );

        return longest.months.slice(0, limitMonths).map((m: any) => {
            const d = new Date(m.date);
            return d.toLocaleDateString("de-DE", {
                year: "2-digit",
                month: "2-digit",
            });
        });
    });

    function formatCurrency(val: number): string {
        return val.toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        });
    }

    function getScenarioEvents(projection: any, activeMetric: string) {
        if (!projection || !projection.months || projection.months.length === 0) {
            return { events: [], milestoneIndices: new Set<number>() };
        }

        const limitMonths = Math.min(
            projection.months.length,
            timeHorizonYears * 12,
        );
        const months = projection.months.slice(0, limitMonths);

        const values = months.map((m: any) => {
            if (activeMetric === "net_worth") {
                return m.assetWorth + m.balance - m.loanDebt;
            } else if (activeMetric === "assets") {
                return m.assetWorth;
            } else if (activeMetric === "cash") {
                return m.balance;
            } else if (activeMetric === "debt") {
                return m.loanDebt;
            }
            return 0;
        });

        interface DeltaCandidate {
            index: number;
            delta: number;
            absDelta: number;
        }

        const deltas: DeltaCandidate[] = [];
        for (let i = 1; i < values.length; i++) {
            deltas.push({
                index: i,
                delta: values[i] - values[i - 1],
                absDelta: Math.abs(values[i] - values[i - 1]),
            });
        }

        // We only consider changes that are substantial (at least €2000)
        const significantChanges = deltas.filter((d) => d.absDelta >= 2000);
        significantChanges.sort((a, b) => b.absDelta - a.absDelta);

        // Limit to top 4 events per scenario to keep timeline neat
        const topChanges = significantChanges.slice(0, 4);

        interface EventInfo {
            index: number;
            dateLabel: string;
            title: string;
            description: string;
            changeAmount: number;
        }

        const events: EventInfo[] = [];
        const milestoneIndices = new Set<number>();

        topChanges.forEach(({ index, delta }) => {
            const currentMonth = months[index];
            const prevMonth = months[index - 1];
            const dateLabel = new Date(currentMonth.date).toLocaleDateString(
                "de-DE",
                {
                    year: "2-digit",
                    month: "2-digit",
                },
            );

            let title = "";
            let description = "";

            if (activeMetric === "debt") {
                const prevLoans: any[] = prevMonth.breakdown?.loans || [];
                const currLoans: any[] = currentMonth.breakdown?.loans || [];

                const paidOff = prevLoans.find((pl) => {
                    const cl = currLoans.find(
                        (c) => c.entityName === pl.entityName,
                    );
                    return pl.balance > 0 && (!cl || cl.balance <= 0);
                });

                if (paidOff) {
                    title = "Loan Paid Off";
                    description = `"${paidOff.entityName}" was fully paid off.`;
                } else {
                    const biggestDrop = currLoans.reduce(
                        (biggest, cl) => {
                            const pl = prevLoans.find(
                                (p) => p.entityName === cl.entityName,
                            );
                            if (pl) {
                                const drop = pl.balance - cl.balance;
                                if (drop > biggest.drop) {
                                    return { name: cl.entityName, drop };
                                }
                            }
                            return biggest;
                        },
                        { name: "", drop: 0 },
                    );

                    if (biggestDrop.drop > 5000) {
                        title = "Major Loan Paydown";
                        description = `Paid off €${biggestDrop.drop.toLocaleString("de-DE", { maximumFractionDigits: 0 })} from "${biggestDrop.name}".`;
                    } else {
                        title = "Debt Reduction";
                        description = `Debt decreased by €${Math.abs(delta).toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                    }
                }
            } else if (activeMetric === "assets") {
                const prevAssets: any[] = prevMonth.breakdown?.assets || [];
                const currAssets: any[] = currentMonth.breakdown?.assets || [];

                const soldAsset = prevAssets.find((pa) => {
                    const ca = currAssets.find(
                        (c) => c.entityName === pa.entityName,
                    );
                    return pa.balance > 0 && (!ca || ca.balance <= 0);
                });

                if (soldAsset) {
                    title = "Asset Liquidated";
                    description = `"${soldAsset.entityName}" was fully liquidated or dumped.`;
                } else {
                    const biggestChange = currAssets.reduce(
                        (biggest, ca) => {
                            const pa = prevAssets.find(
                                (p) => p.entityName === ca.entityName,
                            );
                            if (pa) {
                                const change = ca.balance - pa.balance;
                                if (
                                    Math.abs(change) > Math.abs(biggest.change)
                                ) {
                                    return { name: ca.entityName, change };
                                }
                            }
                            return biggest;
                        },
                        { name: "", change: 0 },
                    );

                    if (biggestChange.change > 5000) {
                        title = "Asset Growth Surge";
                        description = `"${biggestChange.name}" increased by €${biggestChange.change.toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                    } else if (biggestChange.change < -5000) {
                        title = "Asset Payout/Reduction";
                        description = `"${biggestChange.name}" decreased by €${Math.abs(biggestChange.change).toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                    } else {
                        title =
                            delta > 0 ? "Asset Value Growth" : "Asset Value Drop";
                        description = `Assets changed by €${delta.toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                    }
                }
            } else if (activeMetric === "cash") {
                const currBills: any[] = currentMonth.breakdown?.bills || [];
                const currIncomes: any[] =
                    currentMonth.breakdown?.incomes || [];

                const biggestBill = currBills.reduce(
                    (max, b) => (b.amount > max.amount ? b : max),
                    { entityName: "", amount: 0 },
                );
                const biggestIncome = currIncomes.reduce(
                    (max, i) => (i.amount > max.amount ? i : max),
                    { entityName: "", amount: 0 },
                );

                if (delta < 0 && biggestBill.amount > 2000) {
                    title = "Large Outflow";
                    description = `"${biggestBill.entityName}" outflow of €${biggestBill.amount.toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                } else if (delta > 0 && biggestIncome.amount > 2000) {
                    title = "Large Inflow";
                    description = `"${biggestIncome.entityName}" inflow of €${biggestIncome.amount.toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                } else {
                    title =
                        delta > 0 ? "Cash Balance Surge" : "Cash Balance Drop";
                    description = `Cash balance changed by €${delta.toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
                }
            } else {
                title = delta > 0 ? "Net Worth Surge" : "Net Worth Decline";
                description = `Net worth changed by €${delta.toLocaleString("de-DE", { maximumFractionDigits: 0 })}.`;
            }

            events.push({
                index,
                dateLabel,
                title,
                description,
                changeAmount: delta,
            });
            milestoneIndices.add(index);
        });

        events.sort((a, b) => a.index - b.index);
        return { events, milestoneIndices };
    }

    let chartData = $derived.by(() => {
        const datasets = selectedScenarioIds.map((id, index) => {
            const scenario = scenarios.find((s) => s.id === id);
            const projection = projections[id];
            const color = PALETTE[index % PALETTE.length];

            if (!projection || !projection.months) {
                return {
                    label: scenario?.name || "Loading...",
                    data: [],
                    borderColor: color.border,
                    backgroundColor: color.fill,
                    borderWidth: 3,
                    pointRadius: 0,
                    pointHoverRadius: 6,
                    tension: 0.35,
                    fill: true,
                };
            }

            const limitMonths = Math.min(
                projection.months.length,
                timeHorizonYears * 12,
            );
            const dataPoints = projection.months
                .slice(0, limitMonths)
                .map((m: any) => {
                    if (activeMetric === "net_worth") {
                        return m.assetWorth + m.balance - m.loanDebt;
                    } else if (activeMetric === "assets") {
                        return m.assetWorth;
                    } else if (activeMetric === "cash") {
                        return m.balance;
                    } else if (activeMetric === "debt") {
                        return m.loanDebt;
                    }
                    return 0;
                });

            const { milestoneIndices } = getScenarioEvents(
                projection,
                activeMetric,
            );

            return {
                label: scenario?.name || "Scenario",
                data: dataPoints,
                borderColor: color.border,
                backgroundColor: color.fill,
                borderWidth: 3,
                pointRadius: dataPoints.map((_, idx) =>
                    milestoneIndices.has(idx) ? 7 : 0,
                ),
                pointHoverRadius: dataPoints.map((_, idx) =>
                    milestoneIndices.has(idx) ? 9 : 6,
                ),
                pointBackgroundColor: dataPoints.map((_, idx) =>
                    milestoneIndices.has(idx) ? "#ffffff" : color.border,
                ),
                pointBorderWidth: dataPoints.map((_, idx) =>
                    milestoneIndices.has(idx) ? 3 : 0,
                ),
                pointBorderColor: color.border,
                tension: 0.35,
                fill: true,
            };
        });

        return {
            labels: chartLabels,
            datasets,
        };
    });

    // Technical calculations for comparative cards
    interface ScenarioStats {
        id: string;
        name: string;
        endingMetric: number;
        endingNetWorth: number;
        peakAssets: number;
        debtFreedomDate: string;
        fiDate: string;
        endingCash: number;
        color: (typeof PALETTE)[0];
    }

    let scenarioStatsList = $derived.by<ScenarioStats[]>(() => {
        return selectedScenarioIds.map((id, index) => {
            const scenario = scenarios.find((s) => s.id === id);
            const proj = projections[id];
            const color = PALETTE[index % PALETTE.length];

            if (!proj || !proj.months || proj.months.length === 0) {
                return {
                    id,
                    name: scenario?.name || "Loading...",
                    endingMetric: 0,
                    endingNetWorth: 0,
                    peakAssets: 0,
                    debtFreedomDate: "Loading...",
                    fiDate: "Loading...",
                    endingCash: 0,
                    color,
                };
            }

            const limitMonths = Math.min(
                proj.months.length,
                timeHorizonYears * 12,
            );
            const activeMonths = proj.months.slice(0, limitMonths);
            const lastMonth = activeMonths[activeMonths.length - 1];

            const endingCash = lastMonth.balance;
            const endingNetWorth =
                lastMonth.assetWorth + lastMonth.balance - lastMonth.loanDebt;

            let endingMetric = 0;
            if (activeMetric === "net_worth") endingMetric = endingNetWorth;
            else if (activeMetric === "assets")
                endingMetric = lastMonth.assetWorth;
            else if (activeMetric === "cash") endingMetric = endingCash;
            else if (activeMetric === "debt")
                endingMetric = lastMonth.loanDebt;

            let peakAssets = 0;
            let firstZeroDebtIndex = -1;
            let firstFIIndex = -1;

            activeMonths.forEach((m: any, idx: number) => {
                if (m.assetWorth > peakAssets) {
                    peakAssets = m.assetWorth;
                }
                if (m.loanDebt <= 0.05 && firstZeroDebtIndex === -1) {
                    firstZeroDebtIndex = idx;
                }
                if (
                    m.passiveIncome >= m.income &&
                    m.income > 0 &&
                    firstFIIndex === -1
                ) {
                    firstFIIndex = idx;
                }
            });

            const initialDebt = activeMonths[0].loanDebt;
            let debtFreedomDate = "No Debt";

            if (initialDebt > 0) {
                if (firstZeroDebtIndex !== -1) {
                    const zeroMonth = activeMonths[firstZeroDebtIndex];
                    const d = new Date(zeroMonth.date);
                    debtFreedomDate = d.toLocaleDateString("de-DE", {
                        year: "numeric",
                        month: "2-digit",
                    });
                } else {
                    debtFreedomDate = "Never";
                }
            }

            let fiDate = "Never";
            if (firstFIIndex !== -1) {
                const fiMonth = activeMonths[firstFIIndex];
                const d = new Date(fiMonth.date);
                fiDate = d.toLocaleDateString("de-DE", {
                    year: "numeric",
                    month: "2-digit",
                });
            }

            return {
                id,
                name: scenario?.name || "Scenario",
                endingMetric,
                endingNetWorth,
                peakAssets,
                debtFreedomDate,
                fiDate,
                endingCash,
                color,
            };
        });
    });

    const chartOptions = {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: false,
            },
            tooltip: {
                backgroundColor: "rgba(15, 23, 42, 0.9)",
                titleFont: {
                    size: 12,
                    weight: "bold" as const,
                    family: "system-ui",
                },
                bodyFont: { size: 12, family: "system-ui" },
                padding: 12,
                cornerRadius: 12,
                displayColors: true,
                callbacks: {
                    label: function (context: any) {
                        let label = context.dataset.label || "";
                        if (label) {
                            label += ": ";
                        }
                        if (context.parsed.y !== null) {
                            label +=
                                "€ " +
                                context.parsed.y.toLocaleString("de-DE", {
                                    minimumFractionDigits: 2,
                                    maximumFractionDigits: 2,
                                });
                        }
                        return label;
                    },
                    footer: function (tooltipItems: any) {
                        const item = tooltipItems[0];
                        const datasetIndex = item.datasetIndex;
                        const dataIndex = item.dataIndex;
                        const scenarioId = selectedScenarioIds[datasetIndex];
                        const projection = projections[scenarioId];
                        const { events } = getScenarioEvents(
                            projection,
                            activeMetric,
                        );
                        const ev = events.find((e) => e.index === dataIndex);
                        if (ev) {
                            return `\nMilestone: ${ev.title}\n${ev.description}`;
                        }
                        return "";
                    },
                },
            },
        },
        scales: {
            x: {
                grid: {
                    display: false,
                },
                ticks: {
                    color: "#94a3b8",
                    font: { size: 10, weight: "bold" as const },
                    maxTicksLimit: 12,
                },
            },
            y: {
                grid: {
                    color: "#f1f5f9",
                },
                ticks: {
                    color: "#94a3b8",
                    font: { size: 10, weight: "bold" as const },
                    callback: function (value: any) {
                        if (value >= 1e6)
                            return "€" + (value / 1e6).toFixed(1) + "M";
                        if (value >= 1e3)
                            return "€" + (value / 1e3).toFixed(0) + "k";
                        return "€" + value;
                    },
                },
            },
        },
    };

    let activeTab = $state<"projection" | "real" | "contribution">(
        "projection",
    );
    let selectedRealMonthIndex = $state<number>(0);

    let firstScenarioId = $derived(selectedScenarioIds[0] || "");
    let firstProjection = $derived(projections[firstScenarioId] || null);

    let monthsWithRealData = $derived.by(() => {
        if (!firstProjection || !firstProjection.months) return [];
        return firstProjection.months.filter((m: any) => {
            const bd = m.breakdown;
            if (!bd) return false;
            const allEntries = [
                ...(bd.incomes || []),
                ...(bd.bills || []),
                ...(bd.expenses || []),
                ...(bd.assets || []),
                ...(bd.loans || []),
            ];
            return allEntries.some(
                (e: any) =>
                    e.realtimeBalance !== undefined &&
                    e.realtimeBalance !== null,
            );
        });
    });

    $effect(() => {
        if (selectedRealMonthIndex >= monthsWithRealData.length) {
            selectedRealMonthIndex = 0;
        }
    });

    let selectedRealMonth = $derived(
        monthsWithRealData[selectedRealMonthIndex] || null,
    );

    let realVsPlannedItems = $derived.by(() => {
        if (!selectedRealMonth || !selectedRealMonth.breakdown) {
            return {
                incomes: [],
                spendings: [],
                totalPlannedIncome: 0,
                totalRealIncome: 0,
                totalPlannedSpending: 0,
                totalRealSpending: 0,
                plannedNet: 0,
                realNet: 0,
                varianceNet: 0,
            };
        }
        const bd = selectedRealMonth.breakdown;

        const processEntries = (entries: any[], type: string) => {
            return (entries || [])
                .filter(
                    (e: any) =>
                        e.poolId !== undefined &&
                        e.poolId !== null &&
                        e.poolId !== "",
                )
                .map((e: any) => {
                    const planned = e.amount || 0;
                    const real =
                        e.realtimeBalance !== undefined &&
                        e.realtimeBalance !== null
                            ? e.realtimeBalance
                            : 0;
                    const variance = real - planned;
                    const variancePct =
                        planned !== 0 ? (variance / planned) * 100 : 0;
                    return {
                        name: e.name || e.entityName || "Unnamed",
                        type,
                        planned,
                        real,
                        variance,
                        variancePct,
                    };
                });
        };

        const incomes = processEntries(bd.incomes, "Income");
        const spendings = [
            ...processEntries(bd.bills, "Bill"),
            ...processEntries(bd.expenses, "Expense"),
            ...processEntries(bd.loans, "Loan"),
            ...processEntries(bd.assets, "Asset"),
        ];

        const totalPlannedIncome = incomes.reduce(
            (acc, x) => acc + x.planned,
            0,
        );
        const totalRealIncome = incomes.reduce((acc, x) => acc + x.real, 0);

        const totalPlannedSpending = spendings.reduce(
            (acc, x) => acc + x.planned,
            0,
        );
        const totalRealSpending = spendings.reduce((acc, x) => acc + x.real, 0);

        const plannedNet = totalPlannedIncome - totalPlannedSpending;
        const realNet = totalRealIncome - totalRealSpending;
        const varianceNet = realNet - plannedNet;

        return {
            incomes,
            spendings,
            totalPlannedIncome,
            totalRealIncome,
            totalPlannedSpending,
            totalRealSpending,
            plannedNet,
            realNet,
            varianceNet,
        };
    });

    let realChartData = $derived.by(() => {
        if (monthsWithRealData.length === 0) return null;

        const labels = monthsWithRealData.map((m: any) => {
            const d = new Date(m.date);
            return d.toLocaleDateString("de-DE", {
                year: "2-digit",
                month: "2-digit",
            });
        });

        const plannedIncomes: number[] = [];
        const realIncomes: number[] = [];
        const plannedSpendings: number[] = [];
        const realSpendings: number[] = [];

        monthsWithRealData.forEach((m: any) => {
            const bd = m.breakdown || {};
            const filterLinked = (arr: any[]) =>
                (arr || []).filter(
                    (x) =>
                        x.poolId !== undefined &&
                        x.poolId !== null &&
                        x.poolId !== "",
                );
            const sumPlanned = (arr: any[]) =>
                filterLinked(arr).reduce((acc, x) => acc + (x.amount || 0), 0);
            const sumReal = (arr: any[]) =>
                filterLinked(arr).reduce(
                    (acc, x) =>
                        acc +
                        (x.realtimeBalance !== undefined &&
                        x.realtimeBalance !== null
                            ? x.realtimeBalance
                            : 0),
                    0,
                );

            plannedIncomes.push(sumPlanned(bd.incomes));
            realIncomes.push(sumReal(bd.incomes));

            const spendingPlanned =
                sumPlanned(bd.bills) +
                sumPlanned(bd.expenses) +
                sumPlanned(bd.loans) +
                sumPlanned(bd.assets);
            const spendingReal =
                sumReal(bd.bills) +
                sumReal(bd.expenses) +
                sumReal(bd.loans) +
                sumReal(bd.assets);

            plannedSpendings.push(spendingPlanned);
            realSpendings.push(spendingReal);
        });

        return {
            labels,
            datasets: [
                {
                    label: "Planned Income",
                    data: plannedIncomes,
                    backgroundColor: "rgba(99, 102, 241, 0.4)",
                    borderColor: "#6366f1",
                    borderWidth: 2,
                    borderRadius: 6,
                },
                {
                    label: "Real Income",
                    data: realIncomes,
                    backgroundColor: "rgba(16, 185, 129, 0.8)",
                    borderColor: "#10b981",
                    borderWidth: 2,
                    borderRadius: 6,
                },
                {
                    label: "Planned Spending",
                    data: plannedSpendings,
                    backgroundColor: "rgba(244, 63, 94, 0.4)",
                    borderColor: "#f43f5e",
                    borderWidth: 2,
                    borderRadius: 6,
                },
                {
                    label: "Real Spending",
                    data: realSpendings,
                    backgroundColor: "rgba(239, 68, 68, 0.8)",
                    borderColor: "#ef4444",
                    borderWidth: 2,
                    borderRadius: 6,
                },
            ],
        };
    });

    const realChartOptions = {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: true,
                position: "top" as const,
                labels: {
                    boxWidth: 10,
                    font: {
                        size: 10,
                        weight: "bold" as const,
                        family: "system-ui",
                    },
                    color: "#64748b",
                },
            },
            tooltip: {
                backgroundColor: "rgba(15, 23, 42, 0.95)",
                titleFont: {
                    size: 12,
                    weight: "bold" as const,
                    family: "system-ui",
                },
                bodyFont: { size: 12, family: "system-ui" },
                padding: 12,
                cornerRadius: 12,
                displayColors: true,
                callbacks: {
                    label: function (context: any) {
                        let label = context.dataset.label || "";
                        if (label) label += ": ";
                        if (context.parsed.y !== null) {
                            label +=
                                "€ " +
                                context.parsed.y.toLocaleString("de-DE", {
                                    minimumFractionDigits: 2,
                                    maximumFractionDigits: 2,
                                });
                        }
                        return label;
                    },
                },
            },
        },
        scales: {
            x: {
                grid: { display: false },
                ticks: {
                    color: "#94a3b8",
                    font: { size: 10, weight: "bold" as const },
                },
            },
            y: {
                grid: { color: "#f1f5f9" },
                ticks: {
                    color: "#94a3b8",
                    font: { size: 10, weight: "bold" as const },
                    callback: function (value: any) {
                        if (value >= 1e6)
                            return "€" + (value / 1e6).toFixed(1) + "M";
                        if (value >= 1e3)
                            return "€" + (value / 1e3).toFixed(0) + "k";
                        return "€" + value;
                    },
                },
            },
        },
    };

    let contributionData = $derived.by(() => {
        if (!firstProjection || !firstProjection.months) {
            return {
                incomeChart: null,
                spendingChart: null,
                topIncomes: [],
                topSpendings: [],
                totalIncome: 0,
                totalSpending: 0,
                incomeColors: [],
                spendingColors: [],
            };
        }

        const limitMonths = Math.min(
            firstProjection.months.length,
            timeHorizonYears * 12,
        );
        const activeMonths = firstProjection.months.slice(0, limitMonths);

        const incomeMap: Record<string, number> = {};
        const spendingMap: Record<string, { amount: number; type: string }> =
            {};

        activeMonths.forEach((m: any) => {
            const bd = m.breakdown || {};

            (bd.incomes || []).forEach((e: any) => {
                const key = e.name || "Other Income";
                incomeMap[key] = (incomeMap[key] || 0) + (e.amount || 0);
            });

            const addSpending = (entries: any[], type: string) => {
                (entries || []).forEach((e: any) => {
                    const key = e.name || e.entityName || "Other Spending";
                    if (!spendingMap[key]) {
                        spendingMap[key] = { amount: 0, type };
                    }
                    spendingMap[key].amount += e.amount || 0;
                });
            };

            addSpending(bd.bills, "Bill");
            addSpending(bd.expenses, "Expense");
            addSpending(bd.loans, "Loan");
            addSpending(bd.assets, "Asset");
        });

        const topIncomes = Object.entries(incomeMap)
            .map(([name, amount]) => ({ name, amount }))
            .filter((x) => x.amount > 0)
            .sort((a, b) => b.amount - a.amount);

        const topSpendings = Object.entries(spendingMap)
            .map(([name, val]) => ({
                name,
                amount: val.amount,
                type: val.type,
            }))
            .filter((x) => x.amount > 0)
            .sort((a, b) => b.amount - a.amount);

        const totalIncome = topIncomes.reduce((acc, x) => acc + x.amount, 0);
        const totalSpending = topSpendings.reduce(
            (acc, x) => acc + x.amount,
            0,
        );

        const incomeColors = [
            "#10b981",
            "#059669",
            "#34d399",
            "#6366f1",
            "#4f46e5",
            "#818cf8",
            "#06b6d4",
            "#0891b2",
        ];
        const spendingColors = [
            "#f43f5e",
            "#e11d48",
            "#fb7185",
            "#f59e0b",
            "#d97706",
            "#fbbf24",
            "#ef4444",
            "#dc2626",
            "#64748b",
            "#475569",
        ];

        const incomeChart = {
            labels: topIncomes.map((x) => x.name),
            datasets: [
                {
                    data: topIncomes.map((x) => x.amount),
                    backgroundColor: topIncomes.map(
                        (_, i) => incomeColors[i % incomeColors.length],
                    ),
                    borderWidth: 2,
                    borderColor: "#ffffff",
                    hoverOffset: 12,
                },
            ],
        };

        const spendingChart = {
            labels: topSpendings.map((x) => x.name),
            datasets: [
                {
                    data: topSpendings.map((x) => x.amount),
                    backgroundColor: topSpendings.map(
                        (_, i) => spendingColors[i % spendingColors.length],
                    ),
                    borderWidth: 2,
                    borderColor: "#ffffff",
                    hoverOffset: 12,
                },
            ],
        };

        return {
            incomeChart,
            spendingChart,
            topIncomes,
            topSpendings,
            totalIncome,
            totalSpending,
            incomeColors,
            spendingColors,
        };
    });

    const contributionChartOptions = {
        responsive: true,
        maintainAspectRatio: false,
        cutout: "70%",
        plugins: {
            legend: {
                display: false,
            },
            tooltip: {
                backgroundColor: "rgba(15, 23, 42, 0.95)",
                titleFont: {
                    size: 12,
                    weight: "bold" as const,
                    family: "system-ui",
                },
                bodyFont: { size: 12, family: "system-ui" },
                padding: 12,
                cornerRadius: 12,
                displayColors: true,
                callbacks: {
                    label: function (context: any) {
                        const val = context.parsed || 0;
                        const total = context.dataset.data.reduce(
                            (acc: number, x: number) => acc + x,
                            0,
                        );
                        const pct =
                            total > 0
                                ? ((val / total) * 100).toFixed(1)
                                : "0.0";
                        return `${context.label}: € ${val.toLocaleString(
                            "de-DE",
                            {
                                minimumFractionDigits: 2,
                                maximumFractionDigits: 2,
                            },
                        )} (${pct}%)`;
                    },
                },
            },
        },
    };

    function formatGermanAmount(val: number): string {
        return val.toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        });
    }
</script>

<svelte:head>
    <title>Analytics &amp; Projection — BudgetScript</title>
</svelte:head>

<div class="space-y-12 pb-20">
    <header
        class="flex flex-col md:flex-row md:items-end justify-between gap-6"
    >
        <div class="space-y-2">
            <div class="flex items-center gap-2 text-indigo-600">
                <TrendingUp class="w-4 h-4" />
                <span class="text-xs font-black uppercase tracking-[0.2em]"
                    >Deterministic Analytics</span
                >
            </div>
            <h1 class="text-5xl font-black tracking-tight text-slate-900">
                Scenario <span class="gradient-text">Delta</span>.
            </h1>
            <p class="text-slate-500 font-medium text-lg">
                Compare long-term metrics and overlay multiple allocations
                visually.
            </p>
        </div>
    </header>

    {#if isInitialLoading}
        <div
            class="glass-card p-20 flex flex-col items-center justify-center space-y-4"
        >
            <Loader2 class="w-12 h-12 text-indigo-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-xs"
            >
                Assembling Analytics Node...
            </p>
        </div>
    {:else if scenarios.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex p-4 bg-slate-50 rounded-3xl border border-slate-100 shadow-sm"
            >
                <Layers class="w-8 h-8 text-slate-300" />
            </div>
            <div class="space-y-2 max-w-sm mx-auto">
                <h3
                    class="font-black text-slate-900 text-lg uppercase tracking-wider"
                >
                    Awaiting Scenario Context
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    You need at least one custom budget scenario in the engine
                    to begin comparing projection lines.
                </p>
            </div>
            <a href="/scenarios" class="btn-primary py-3.5 px-8 inline-flex"
                >Initialize Scenario Manager</a
            >
        </div>
    {:else}
        <div class="bento-grid">
            <!-- Sidebar Controls -->
            <div class="md:col-span-3 space-y-6">
                <!-- Select Scenarios -->
                <div class="glass-card p-6 space-y-4">
                    <label
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                        >Overlay Scope</label
                    >
                    <div class="space-y-2">
                        {#each scenarios as s, idx}
                            {@const isSelected = selectedScenarioIds.includes(
                                s.id,
                            )}
                            {@const color =
                                PALETTE[
                                    selectedScenarioIds.indexOf(s.id) %
                                        PALETTE.length
                                ]}
                            <button
                                onclick={() => toggleScenarioSelection(s.id)}
                                class="w-full flex items-center justify-between p-3.5 rounded-2xl border text-left font-bold text-xs transition-all relative overflow-hidden
                                       {isSelected
                                    ? 'bg-white border-slate-200 shadow-sm ring-1 ring-slate-100'
                                    : 'bg-slate-50/50 border-slate-100 text-slate-500 hover:bg-slate-50'}"
                            >
                                {#if isSelected}
                                    <div
                                        class="absolute left-0 top-0 w-1.5 h-full"
                                        style="background-color: {color.border}"
                                    ></div>
                                {/if}
                                <span
                                    class="pl-2 pr-4 truncate {isSelected
                                        ? 'text-slate-900 font-black'
                                        : ''}">{s.name}</span
                                >
                                {#if isSelected}
                                    <span
                                        class="px-2 py-0.5 rounded-full text-[9px] font-black uppercase tracking-tighter"
                                        style="background-color: {color.border}22; color: {color.border}"
                                    >
                                        Line {selectedScenarioIds.indexOf(
                                            s.id,
                                        ) + 1}
                                    </span>
                                {/if}
                            </button>
                        {/each}
                    </div>
                </div>

                <!-- Metric Toggles -->
                <div class="glass-card p-6 space-y-4">
                    <label
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                        >Focus Metric</label
                    >
                    <div class="grid grid-cols-1 gap-2">
                        <button
                            onclick={() => (activeMetric = "net_worth")}
                            class="flex items-center gap-3 p-3.5 rounded-2xl border font-bold text-xs transition-all
                                   {activeMetric === 'net_worth'
                                ? 'bg-indigo-600 border-indigo-600 text-white shadow-lg shadow-indigo-100'
                                : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-50'}"
                        >
                            <Activity class="w-4 h-4" />
                            Net Worth
                        </button>
                        <button
                            onclick={() => (activeMetric = "assets")}
                            class="flex items-center gap-3 p-3.5 rounded-2xl border font-bold text-xs transition-all
                                   {activeMetric === 'assets'
                                ? 'bg-emerald-600 border-emerald-600 text-white shadow-lg shadow-emerald-100'
                                : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-50'}"
                        >
                            <PieChart class="w-4 h-4" />
                            Asset Worth
                        </button>
                        <button
                            onclick={() => (activeMetric = "cash")}
                            class="flex items-center gap-3 p-3.5 rounded-2xl border font-bold text-xs transition-all
                                   {activeMetric === 'cash'
                                ? 'bg-amber-600 border-amber-600 text-white shadow-lg shadow-amber-100'
                                : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-50'}"
                        >
                            <Wallet class="w-4 h-4" />
                            Liquid Cash
                        </button>
                        <button
                            onclick={() => (activeMetric = "debt")}
                            class="flex items-center gap-3 p-3.5 rounded-2xl border font-bold text-xs transition-all
                                   {activeMetric === 'debt'
                                ? 'bg-slate-900 border-slate-900 text-white shadow-lg shadow-slate-200'
                                : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-50'}"
                        >
                            <HandCoins class="w-4 h-4" />
                            Loan Debt
                        </button>
                    </div>
                </div>

                <!-- Time Horizon Selection -->
                <div class="glass-card p-6 space-y-4">
                    <label
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                        >Time Horizon</label
                    >
                    <div class="grid grid-cols-2 gap-2">
                        {#each [5, 10, 20, 30] as y}
                            <button
                                onclick={() => (timeHorizonYears = y)}
                                class="p-3 rounded-2xl border font-black text-[10px] uppercase tracking-wider transition-all
                                       {timeHorizonYears === y
                                    ? 'bg-slate-900 border-slate-900 text-white shadow-lg shadow-slate-200'
                                    : 'bg-white border-slate-200 text-slate-500 hover:bg-slate-50'}"
                            >
                                {y} Years
                            </button>
                        {/each}
                    </div>
                </div>
            </div>

            <!-- Visualization Canvas -->
            <div
                class="md:col-span-9 glass-card p-10 flex flex-col space-y-8 min-h-[500px]"
            >
                <!-- Premium Glassmorphic Tab Switcher -->
                <div
                    class="flex flex-col sm:flex-row sm:items-center justify-between border-b border-slate-100 pb-6 gap-4"
                >
                    <div class="flex items-center gap-2.5">
                        <button
                            onclick={() => (activeTab = "projection")}
                            class="px-4 py-2 rounded-xl text-[10px] font-black uppercase tracking-wider transition-all
                                   {activeTab === 'projection'
                                ? 'bg-slate-900 text-white shadow-md'
                                : 'bg-slate-50 text-slate-400 hover:text-slate-600 hover:bg-slate-100'}"
                        >
                            Projections
                        </button>
                        <button
                            onclick={() => (activeTab = "real")}
                            class="px-4 py-2 rounded-xl text-[10px] font-black uppercase tracking-wider transition-all
                                   {activeTab === 'real'
                                ? 'bg-slate-900 text-white shadow-md'
                                : 'bg-slate-50 text-slate-400 hover:text-slate-600 hover:bg-slate-100'}"
                        >
                            Real vs Planned
                        </button>
                        <button
                            onclick={() => (activeTab = "contribution")}
                            class="px-4 py-2 rounded-xl text-[10px] font-black uppercase tracking-wider transition-all
                                   {activeTab === 'contribution'
                                ? 'bg-slate-900 text-white shadow-md'
                                : 'bg-slate-50 text-slate-400 hover:text-slate-600 hover:bg-slate-100'}"
                        >
                            Contributions
                        </button>
                    </div>

                    <div>
                        {#if activeTab === "projection"}
                            <div
                                class="flex flex-wrap gap-4 max-w-[280px] justify-end"
                            >
                                {#each selectedScenarioIds as id, index}
                                    {@const scenario = scenarios.find(
                                        (s) => s.id === id,
                                    )}
                                    {@const color =
                                        PALETTE[index % PALETTE.length]}
                                    <div
                                        class="flex items-center gap-2 text-[10px] font-black text-slate-600"
                                    >
                                        <span
                                            class="w-2.5 h-2.5 rounded-full"
                                            style="background-color: {color.border}"
                                        ></span>
                                        <span class="truncate max-w-[100px]"
                                            >{scenario?.name ||
                                                "Scenario"}</span
                                        >
                                    </div>
                                {/each}
                            </div>
                        {:else if activeTab === "real"}
                            <span
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >Budget Implementation</span
                            >
                        {:else}
                            <span
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >{timeHorizonYears}-Year Horizon</span
                            >
                        {/if}
                    </div>
                </div>

                <!-- Tab 1: Projections (Time Series) -->
                {#if activeTab === "projection"}
                    <div class="flex items-center justify-between">
                        <div>
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                >Time Series Projection</label
                            >
                            <h3
                                class="text-2xl font-black text-slate-900 tracking-tight capitalize"
                            >
                                Plotted Delta: {activeMetric.replace("_", " ")}
                            </h3>
                        </div>
                    </div>

                    <div class="flex-1 relative min-h-[350px]">
                        {#if Object.values(loadingProjections).some((x) => x)}
                            <div
                                class="absolute inset-0 flex flex-col items-center justify-center space-y-4 bg-white/50 backdrop-blur-sm z-10 rounded-2xl"
                            >
                                <Loader2
                                    class="w-10 h-10 text-indigo-600 animate-spin"
                                />
                                <p
                                    class="text-slate-400 font-black uppercase tracking-[0.2em] text-[9px] animate-pulse"
                                >
                                    Running Projections...
                                </p>
                            </div>
                        {/if}

                        {#if chartLabels.length > 0}
                            <Line data={chartData} options={chartOptions} />
                        {:else}
                            <div
                                class="absolute inset-0 flex items-center justify-center text-slate-400 font-bold text-sm"
                            >
                                Loading Projection Data...
                            </div>
                        {/if}
                    </div>

                    <!-- Interactive Timeline Section -->
                    {#if chartLabels.length > 0}
                        {@const activeEvents = selectedScenarioIds.map((id, idx) => {
                            const scenario = scenarios.find((s) => s.id === id);
                            const proj = projections[id];
                            const { events } = getScenarioEvents(proj, activeMetric);
                            return {
                                scenarioName: scenario?.name || "Scenario",
                                color: PALETTE[idx % PALETTE.length],
                                events
                            };
                        }).filter((x) => x.events.length > 0)}
                        
                        {#if activeEvents.length > 0}
                            <div class="mt-8 border-t border-slate-100 dark:border-slate-800/40 pt-6 animate-fade-in">
                                <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-400 mb-4 ml-1">
                                    Significant Projection Milestones
                                </h4>
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                    {#each activeEvents as scEvents}
                                        <div class="bg-slate-50/50 dark:bg-slate-900/20 border border-slate-100 dark:border-slate-800/40 rounded-3xl p-5 space-y-4">
                                            <div class="flex items-center gap-2">
                                                <div class="w-2.5 h-2.5 rounded-full" style="background-color: {scEvents.color.border}"></div>
                                                <span class="text-xs font-black uppercase tracking-wider text-slate-900 dark:text-slate-100">{scEvents.scenarioName}</span>
                                            </div>
                                            <div class="relative pl-4 border-l border-slate-200/60 dark:border-slate-800/40 space-y-5 ml-1">
                                                {#each scEvents.events as ev}
                                                    <div class="relative group">
                                                        <!-- Timeline Dot -->
                                                        <div class="absolute -left-[21px] top-1 w-2.5 h-2.5 rounded-full border bg-white dark:bg-slate-950 transition-all group-hover:scale-125" style="border-color: {scEvents.color.border}; background-color: {scEvents.color.border}"></div>
                                                        <div class="space-y-1">
                                                            <div class="flex items-center justify-between">
                                                                <span class="text-[10px] font-black text-slate-900 dark:text-slate-200 uppercase tracking-tight">{ev.title}</span>
                                                                <span class="text-[9px] font-black text-slate-400 uppercase tracking-widest">{ev.dateLabel}</span>
                                                            </div>
                                                            <p class="text-[10px] text-slate-500 dark:text-slate-400 tracking-tight leading-relaxed">{ev.description}</p>
                                                            <div class="text-[9px] font-black uppercase tracking-tight {ev.changeAmount >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'}">
                                                                {ev.changeAmount >= 0 ? '+' : ''}{formatCurrency(ev.changeAmount)} €
                                                            </div>
                                                        </div>
                                                    </div>
                                                {/each}
                                            </div>
                                        </div>
                                    {/each}
                                </div>
                            </div>
                        {/if}
                    {/if}
                {/if}

                <!-- Tab 2: Real vs Planned -->
                {#if activeTab === "real"}
                    {#if monthsWithRealData.length === 0}
                        <div
                            class="flex flex-col items-center justify-center p-16 border border-dashed border-slate-200 rounded-3xl space-y-6 text-center"
                        >
                            <div
                                class="p-4 bg-slate-50 border border-slate-100 rounded-3xl shadow-sm"
                            >
                                <HelpCircle class="w-8 h-8 text-slate-300" />
                            </div>
                            <div class="space-y-2 max-w-md">
                                <h4
                                    class="font-black text-slate-900 text-base uppercase tracking-wider"
                                >
                                    No Real-World Transaction Data Found
                                </h4>
                                <p class="text-slate-500 font-medium text-xs">
                                    Actual transaction balance sync has not been
                                    established yet. To track real spending vs
                                    planned budget, please connect a financial
                                    integration or import transactions.
                                </p>
                            </div>
                            <a
                                href="/scenarios"
                                class="btn-primary py-2.5 px-6 inline-flex text-xs font-bold"
                            >
                                Setup Financial Integration
                            </a>
                        </div>
                    {:else}
                        <!-- Bar chart comparison -->
                        <div class="grid grid-cols-1 lg:grid-cols-3 gap-8">
                            <div class="lg:col-span-2 space-y-4">
                                <h4
                                    class="text-xs font-black uppercase tracking-[0.2em] text-slate-900"
                                >
                                    Historical Planned vs. Real Execution
                                </h4>
                                <div class="h-[220px] relative">
                                    {#if realChartData}
                                        <Bar
                                            data={realChartData}
                                            options={realChartOptions}
                                        />
                                    {/if}
                                </div>
                            </div>

                            <!-- Selected Month switcher and KPI summary cards -->
                            <div
                                class="glass-card p-6 border border-slate-100 flex flex-col justify-between space-y-4"
                            >
                                <div class="space-y-3">
                                    <label
                                        class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                                        >Select Month to Detail</label
                                    >
                                    <select
                                        bind:value={selectedRealMonthIndex}
                                        class="w-full p-2.5 text-xs font-bold text-slate-700 bg-slate-50 border border-slate-200 rounded-xl outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-100 transition-all"
                                    >
                                        {#each monthsWithRealData as m, idx}
                                            <option value={idx}>
                                                {new Date(
                                                    m.date,
                                                ).toLocaleDateString("de-DE", {
                                                    month: "long",
                                                    year: "numeric",
                                                })}
                                            </option>
                                        {/each}
                                    </select>
                                </div>

                                <div
                                    class="space-y-3 pt-3 border-t border-slate-100"
                                >
                                    <!-- Net Variance Card -->
                                    <div
                                        class="flex items-center justify-between text-xs"
                                    >
                                        <span class="text-slate-400 font-bold"
                                            >Planned Net:</span
                                        >
                                        <span class="font-black text-slate-900"
                                            >€ {formatGermanAmount(
                                                realVsPlannedItems.plannedNet,
                                            )}</span
                                        >
                                    </div>
                                    <div
                                        class="flex items-center justify-between text-xs"
                                    >
                                        <span class="text-slate-400 font-bold"
                                            >Real Net:</span
                                        >
                                        <span class="font-black text-slate-900"
                                            >€ {formatGermanAmount(
                                                realVsPlannedItems.realNet,
                                            )}</span
                                        >
                                    </div>
                                    <div
                                        class="flex items-center justify-between text-xs pt-2 border-t border-dashed border-slate-100"
                                    >
                                        <span class="text-slate-500 font-black"
                                            >Net Variance:</span
                                        >
                                        <span
                                            class="font-black {realVsPlannedItems.varianceNet >=
                                            0
                                                ? 'text-emerald-600'
                                                : 'text-rose-600'}"
                                        >
                                            € {realVsPlannedItems.varianceNet >=
                                            0
                                                ? "+"
                                                : ""}{formatGermanAmount(
                                                realVsPlannedItems.varianceNet,
                                            )}
                                        </span>
                                    </div>
                                </div>
                            </div>
                        </div>

                        <!-- Itemized Category Breakdown Tables -->
                        <div class="space-y-6 pt-4">
                            <div class="flex items-center justify-between">
                                <h4
                                    class="text-xs font-black uppercase tracking-[0.2em] text-slate-900"
                                >
                                    Itemized Category Breakdown
                                </h4>
                            </div>

                            <div class="grid grid-cols-1 xl:grid-cols-2 gap-8">
                                <!-- Incomes Table -->
                                <div
                                    class="glass-card border border-slate-100 overflow-hidden"
                                >
                                    <div
                                        class="px-6 py-4 bg-slate-50 border-b border-slate-100 flex justify-between items-center"
                                    >
                                        <h5
                                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900"
                                        >
                                            Income Sources
                                        </h5>
                                        <span
                                            class="text-[10px] font-black text-emerald-600"
                                            >Real Total: € {formatGermanAmount(
                                                realVsPlannedItems.totalRealIncome,
                                            )}</span
                                        >
                                    </div>
                                    <div
                                        class="overflow-x-auto max-h-[300px] overflow-y-auto"
                                    >
                                        <table
                                            class="w-full text-left border-collapse table-fixed"
                                        >
                                            <colgroup>
                                                <col class="w-auto" />
                                                <col class="w-20" />
                                                <col class="w-20" />
                                                <col class="w-24" />
                                            </colgroup>
                                            <thead>
                                                <tr
                                                    class="bg-slate-50/50 border-b border-slate-100 text-[9px] font-black uppercase tracking-[0.1em] text-slate-400"
                                                >
                                                    <th class="px-6 py-3"
                                                        >Category</th
                                                    >
                                                    <th
                                                        class="px-3 py-3 text-right"
                                                        >Planned</th
                                                    >
                                                    <th
                                                        class="px-3 py-3 text-right"
                                                        >Real</th
                                                    >
                                                    <th
                                                        class="px-6 py-3 text-right"
                                                        >Variance</th
                                                    >
                                                </tr>
                                            </thead>
                                            <tbody
                                                class="divide-y divide-slate-50 text-[11px]"
                                            >
                                                {#each realVsPlannedItems.incomes as item}
                                                    <tr
                                                        class="hover:bg-slate-50/50 transition-colors"
                                                    >
                                                        <td
                                                            class="px-6 py-3.5 font-bold text-slate-700 truncate pr-2"
                                                            title={item.name}
                                                            >{item.name}</td
                                                        >
                                                        <td
                                                            class="px-3 py-3.5 text-right font-bold text-slate-400"
                                                            >€{formatGermanAmount(
                                                                item.planned,
                                                            )}</td
                                                        >
                                                        <td
                                                            class="px-3 py-3.5 text-right font-black text-slate-900"
                                                            >€{formatGermanAmount(
                                                                item.real,
                                                            )}</td
                                                        >
                                                        <td
                                                            class="px-6 py-3.5 text-right font-bold"
                                                        >
                                                            <span
                                                                class={item.variance >=
                                                                0
                                                                    ? "text-emerald-600"
                                                                    : "text-rose-500"}
                                                            >
                                                                {item.variance >=
                                                                0
                                                                    ? "+"
                                                                    : ""}{formatGermanAmount(
                                                                    item.variance,
                                                                )}
                                                            </span>
                                                        </td>
                                                    </tr>
                                                {/each}
                                            </tbody>
                                        </table>
                                    </div>
                                </div>

                                <!-- Spendings Table -->
                                <div
                                    class="glass-card border border-slate-100 overflow-hidden"
                                >
                                    <div
                                        class="px-6 py-4 bg-slate-50 border-b border-slate-100 flex justify-between items-center"
                                    >
                                        <h5
                                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900"
                                        >
                                            Spending & Savings
                                        </h5>
                                        <span
                                            class="text-[10px] font-black text-rose-600"
                                            >Real Total: € {formatGermanAmount(
                                                realVsPlannedItems.totalRealSpending,
                                            )}</span
                                        >
                                    </div>
                                    <div
                                        class="overflow-x-auto max-h-[300px] overflow-y-auto"
                                    >
                                        <table
                                            class="w-full text-left border-collapse table-fixed"
                                        >
                                            <colgroup>
                                                <col class="w-auto" />
                                                <col class="w-16" />
                                                <col class="w-20" />
                                                <col class="w-20" />
                                                <col class="w-24" />
                                            </colgroup>
                                            <thead>
                                                <tr
                                                    class="bg-slate-50/50 border-b border-slate-100 text-[9px] font-black uppercase tracking-[0.1em] text-slate-400"
                                                >
                                                    <th class="px-6 py-3"
                                                        >Category</th
                                                    >
                                                    <th class="px-2 py-3"
                                                        >Type</th
                                                    >
                                                    <th
                                                        class="px-3 py-3 text-right"
                                                        >Planned</th
                                                    >
                                                    <th
                                                        class="px-3 py-3 text-right"
                                                        >Real</th
                                                    >
                                                    <th
                                                        class="px-6 py-3 text-right"
                                                        >Variance</th
                                                    >
                                                </tr>
                                            </thead>
                                            <tbody
                                                class="divide-y divide-slate-50 text-[11px]"
                                            >
                                                {#each realVsPlannedItems.spendings as item}
                                                    <tr
                                                        class="hover:bg-slate-50/50 transition-colors"
                                                    >
                                                        <td
                                                            class="px-6 py-3.5 font-bold text-slate-700 truncate pr-2"
                                                            title={item.name}
                                                            >{item.name}</td
                                                        >
                                                        <td
                                                            class="px-2 py-3.5 font-black text-[9px] uppercase tracking-tighter text-slate-400"
                                                            >{item.type}</td
                                                        >
                                                        <td
                                                            class="px-3 py-3.5 text-right font-bold text-slate-400"
                                                            >€{formatGermanAmount(
                                                                item.planned,
                                                            )}</td
                                                        >
                                                        <td
                                                            class="px-3 py-3.5 text-right font-black text-slate-900"
                                                            >€{formatGermanAmount(
                                                                item.real,
                                                            )}</td
                                                        >
                                                        <td
                                                            class="px-6 py-3.5 text-right font-bold"
                                                        >
                                                            <span
                                                                class={item.variance <=
                                                                0
                                                                    ? "text-emerald-600"
                                                                    : "text-rose-500"}
                                                            >
                                                                {item.variance >
                                                                0
                                                                    ? "+"
                                                                    : ""}{formatGermanAmount(
                                                                    item.variance,
                                                                )}
                                                            </span>
                                                        </td>
                                                    </tr>
                                                {/each}
                                            </tbody>
                                        </table>
                                    </div>
                                </div>
                            </div>
                        </div>
                    {/if}
                {/if}

                <!-- Tab 3: Contribution -->
                {#if activeTab === "contribution"}
                    <div class="grid grid-cols-1 lg:grid-cols-2 gap-12">
                        <!-- Income Generation Column -->
                        <div class="space-y-6">
                            <div class="space-y-1">
                                <h4
                                    class="text-xs font-black uppercase tracking-[0.2em] text-slate-900 flex items-center gap-2"
                                >
                                    <TrendingUp
                                        class="w-4 h-4 text-emerald-600"
                                    />
                                    Income Generation
                                </h4>
                                <p class="text-slate-400 font-medium text-xs">
                                    Total revenue generated by each source over
                                    the {timeHorizonYears}-year horizon.
                                </p>
                            </div>

                            <div
                                class="h-[200px] relative flex items-center justify-center"
                            >
                                {#if contributionData.incomeChart && contributionData.topIncomes.length > 0}
                                    <Doughnut
                                        data={contributionData.incomeChart}
                                        options={contributionChartOptions}
                                    />
                                    <div
                                        class="absolute flex flex-col items-center justify-center text-center pointer-events-none"
                                    >
                                        <span
                                            class="text-[9px] font-black text-slate-400 uppercase tracking-widest"
                                            >Total Income</span
                                        >
                                        <span
                                            class="font-black text-slate-900 text-lg"
                                            >€{formatGermanAmount(
                                                contributionData.totalIncome,
                                            )}</span
                                        >
                                    </div>
                                {:else}
                                    <span
                                        class="text-slate-400 font-bold text-xs"
                                        >No Income Data Projected</span
                                    >
                                {/if}
                            </div>

                            <!-- Ranked List -->
                            <div
                                class="space-y-2.5 max-h-[250px] overflow-y-auto pr-1"
                            >
                                {#each contributionData.topIncomes as income, idx}
                                    {@const percentage =
                                        contributionData.totalIncome > 0
                                            ? (income.amount /
                                                  contributionData.totalIncome) *
                                              100
                                            : 0}
                                    {@const color =
                                        contributionData.incomeColors[
                                            idx %
                                                contributionData.incomeColors
                                                    .length
                                        ]}
                                    <div
                                        class="flex items-center justify-between p-3 rounded-xl border border-slate-100 hover:border-slate-200 bg-white hover:bg-slate-50 transition-all shadow-sm"
                                    >
                                        <div
                                            class="flex items-center gap-3 min-w-0"
                                        >
                                            <span
                                                class="flex items-center justify-center w-5 h-5 rounded-lg text-[9px] font-black text-white"
                                                style="background-color: {color}"
                                            >
                                                #{idx + 1}
                                            </span>
                                            <span
                                                class="font-bold text-slate-700 text-xs truncate"
                                                title={income.name}
                                                >{income.name}</span
                                            >
                                        </div>
                                        <div
                                            class="text-right whitespace-nowrap pl-4"
                                        >
                                            <span
                                                class="font-black text-slate-900 text-xs block"
                                                >€{formatGermanAmount(
                                                    income.amount,
                                                )}</span
                                            >
                                            <span
                                                class="text-[9px] font-black text-emerald-600 block"
                                                >{percentage.toFixed(1)}%</span
                                            >
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        </div>

                        <!-- Spending Consumption Column -->
                        <div class="space-y-6">
                            <div class="space-y-1">
                                <h4
                                    class="text-xs font-black uppercase tracking-[0.2em] text-slate-900 flex items-center gap-2"
                                >
                                    <HandCoins class="w-4 h-4 text-rose-500" />
                                    Spending & Savings Hotspots
                                </h4>
                                <p class="text-slate-400 font-medium text-xs">
                                    Total wealth consumed by liabilities,
                                    savings rate, and loans over the {timeHorizonYears}-year
                                    horizon.
                                </p>
                            </div>

                            <div
                                class="h-[200px] relative flex items-center justify-center"
                            >
                                {#if contributionData.spendingChart && contributionData.topSpendings.length > 0}
                                    <Doughnut
                                        data={contributionData.spendingChart}
                                        options={contributionChartOptions}
                                    />
                                    <div
                                        class="absolute flex flex-col items-center justify-center text-center pointer-events-none"
                                    >
                                        <span
                                            class="text-[9px] font-black text-slate-400 uppercase tracking-widest"
                                            >Total Spent</span
                                        >
                                        <span
                                            class="font-black text-slate-900 text-lg"
                                            >€{formatGermanAmount(
                                                contributionData.totalSpending,
                                            )}</span
                                        >
                                    </div>
                                {:else}
                                    <span
                                        class="text-slate-400 font-bold text-xs"
                                        >No Spending Data Projected</span
                                    >
                                {/if}
                            </div>

                            <!-- Ranked List -->
                            <div
                                class="space-y-2.5 max-h-[250px] overflow-y-auto pr-1"
                            >
                                {#each contributionData.topSpendings as spending, idx}
                                    {@const percentage =
                                        contributionData.totalSpending > 0
                                            ? (spending.amount /
                                                  contributionData.totalSpending) *
                                              100
                                            : 0}
                                    {@const color =
                                        contributionData.spendingColors[
                                            idx %
                                                contributionData.spendingColors
                                                    .length
                                        ]}
                                    <div
                                        class="flex items-center justify-between p-3 rounded-xl border border-slate-100 hover:border-slate-200 bg-white hover:bg-slate-50 transition-all shadow-sm"
                                    >
                                        <div
                                            class="flex items-center gap-3 min-w-0"
                                        >
                                            <span
                                                class="flex items-center justify-center w-5 h-5 rounded-lg text-[9px] font-black text-white"
                                                style="background-color: {color}"
                                            >
                                                #{idx + 1}
                                            </span>
                                            <div class="min-w-0">
                                                <span
                                                    class="font-bold text-slate-700 text-xs block truncate pr-1"
                                                    title={spending.name}
                                                    >{spending.name}</span
                                                >
                                                <span
                                                    class="text-[8px] font-black text-slate-400 uppercase tracking-wider block"
                                                    >{spending.type}</span
                                                >
                                            </div>
                                        </div>
                                        <div
                                            class="text-right whitespace-nowrap pl-4"
                                        >
                                            <span
                                                class="font-black text-slate-900 text-xs block"
                                                >€{formatGermanAmount(
                                                    spending.amount,
                                                )}</span
                                            >
                                            <span
                                                class="text-[9px] font-black text-rose-500 block"
                                                >{percentage.toFixed(1)}%</span
                                            >
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        </div>
                    </div>
                {/if}
            </div>

            <!-- Asset Details Explorer -->
            <div
                class="md:col-span-12 glass-card p-10 flex flex-col space-y-8 min-h-[500px]"
            >
                <div
                    class="flex flex-col md:flex-row md:items-center justify-between gap-6 pb-6 border-b border-slate-100"
                >
                    <div class="space-y-1">
                        <div class="flex items-center gap-2 text-indigo-600">
                            <Sparkles class="w-4 h-4" />
                            <span
                                class="text-xs font-black uppercase tracking-[0.2em]"
                                >Asset Drill-down</span
                            >
                        </div>
                        <h3
                            class="text-3xl font-black text-slate-900 tracking-tight"
                        >
                            Asset Details <span class="gradient-text"
                                >Explorer</span
                            >
                        </h3>
                        <p class="text-slate-500 font-medium text-sm">
                            Analyze the month-by-month projection details of a
                            specific asset (Balance, Net Flow, Growth, and
                            Penalties).
                        </p>
                    </div>

                    <div class="flex wrap gap-4 items-center">
                        <!-- Scenario Selector -->
                        <div class="flex flex-col space-y-1.5">
                            <SearchableDropdown
                                label="Scenario"
                                options={selectedScenarioIds.map((id) => ({
                                    id,
                                    label:
                                        scenarios.find((s) => s.id === id)
                                            ?.name || id,
                                }))}
                                bind:value={selectedAssetScenarioId}
                            />
                        </div>

                        <!-- Asset Selector -->
                        <div class="flex flex-col space-y-1.5">
                            <SearchableDropdown
                                label="Asset"
                                options={availableAssets.map((name) => ({
                                    id: name,
                                    label: name,
                                }))}
                                bind:value={selectedAssetName}
                                placeholder={availableAssets.length === 0
                                    ? "No Assets Available"
                                    : "Select Asset..."}
                            />
                        </div>
                    </div>
                </div>

                {#if availableAssets.length > 0 && selectedAssetName}
                    <!-- Quick Summary Metrics -->
                    {#if assetSummary}
                        <div
                            class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-6"
                        >
                            <div
                                class="glass-card p-6 border border-slate-100 flex flex-col space-y-2"
                            >
                                <span
                                    class="text-[9px] font-black text-emerald-600 uppercase tracking-[0.2em]"
                                    >Ending Balance</span
                                >
                                <span
                                    class="font-black text-slate-900 text-2xl"
                                >
                                    € {formatGermanAmount(
                                        assetSummary.endingBalance,
                                    )}
                                </span>
                                <span
                                    class="text-[10px] text-slate-400 font-medium"
                                    >Accumulated value at {timeHorizonYears}y</span
                                >
                            </div>

                            <div
                                class="glass-card p-6 border border-slate-100 flex flex-col space-y-2"
                            >
                                <span
                                    class="text-[9px] font-black text-indigo-600 uppercase tracking-[0.2em]"
                                    >Total Contributions</span
                                >
                                <span
                                    class="font-black text-slate-900 text-2xl"
                                >
                                    € {formatGermanAmount(
                                        assetSummary.totalContributions,
                                    )}
                                </span>
                                <span
                                    class="text-[10px] text-slate-400 font-medium"
                                    >Total capital deposited</span
                                >
                            </div>

                            <div
                                class="glass-card p-6 border border-slate-100 flex flex-col space-y-2"
                            >
                                <span
                                    class="text-[9px] font-black text-amber-600 uppercase tracking-[0.2em]"
                                    >Total Interest / Growth</span
                                >
                                <span
                                    class="font-black text-emerald-600 text-2xl"
                                >
                                    € {formatGermanAmount(
                                        assetSummary.totalInterest,
                                    )}
                                </span>
                                <span
                                    class="text-[10px] text-slate-400 font-medium"
                                    >Total return generated over time</span
                                >
                            </div>

                            <div
                                class="glass-card p-6 border border-slate-100 flex flex-col space-y-2"
                            >
                                <span
                                    class="text-[9px] font-black text-cyan-600 uppercase tracking-[0.2em]"
                                    >Total Withdrawals</span
                                >
                                <span
                                    class="font-black text-slate-900 text-2xl"
                                >
                                    € {formatGermanAmount(
                                        assetSummary.totalWithdrawals,
                                    )}
                                </span>
                                <span
                                    class="text-[10px] text-slate-400 font-medium"
                                    >Total capital withdrawn / paid out</span
                                >
                            </div>

                            <div
                                class="glass-card p-6 border border-slate-100 flex flex-col space-y-2"
                            >
                                <span
                                    class="text-[9px] font-black text-rose-500 uppercase tracking-[0.2em]"
                                    >Total Penalties Paid</span
                                >
                                <span class="font-black text-rose-600 text-2xl">
                                    € {formatGermanAmount(
                                        assetSummary.totalPenalties,
                                    )}
                                </span>
                                <span
                                    class="text-[10px] text-slate-400 font-medium"
                                    >Fees incurred from early withdrawal</span
                                >
                            </div>
                        </div>
                    {/if}

                    <!-- ETF Trackers Allocations Card Grid -->
                    {#if selectedAssetInfo?.activeVersion?.type === "ETF" && selectedAssetInfo?.activeVersion?.etfConfig?.length > 0}
                        <div
                            class="glass-card p-8 border border-emerald-100/50 bg-emerald-50/5 space-y-6"
                        >
                            <div
                                class="flex items-center justify-between border-b border-slate-100 pb-4"
                            >
                                <div class="flex items-center gap-2.5">
                                    <div class="p-2 bg-emerald-100 rounded-xl">
                                        <PieChart
                                            class="w-4 h-4 text-emerald-600"
                                        />
                                    </div>
                                    <div>
                                        <h4
                                            class="text-xs font-black uppercase tracking-[0.2em] text-slate-900"
                                        >
                                            ETF Tracker Allocation Details
                                        </h4>
                                        <p
                                            class="text-[10px] text-slate-400 font-semibold uppercase"
                                        >
                                            Distribution of Worth Contribution
                                        </p>
                                    </div>
                                </div>
                                <span
                                    class="px-2.5 py-1 rounded-full text-[9px] font-black uppercase tracking-wider bg-emerald-50 text-emerald-700 border border-emerald-100"
                                >
                                    {#if finalRealSplit}
                                        Drifted Real Split
                                    {:else}
                                        Target Allocation
                                    {/if}
                                </span>
                            </div>
                            <div
                                class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6"
                            >
                                {#each selectedAssetInfo.activeVersion.etfConfig as tracker}
                                    {@const percentage =
                                        finalRealSplit &&
                                        finalRealSplit[tracker.tracker] !==
                                            undefined
                                            ? finalRealSplit[tracker.tracker]
                                            : tracker.percentage}
                                    {@const endingWorth =
                                        (assetSummary?.endingBalance || 0) *
                                        percentage}
                                    {@const totalCont =
                                        trackerCumulativeFlows[
                                            tracker.tracker
                                        ] !== undefined
                                            ? trackerCumulativeFlows[
                                                  tracker.tracker
                                              ]
                                            : (assetSummary?.totalContributions ||
                                                  0) * percentage}
                                    {@const totalGrowth =
                                        endingWorth - totalCont}
                                    {@const trackerYield =
                                        projections[selectedAssetScenarioId]
                                            ?.simulated_yields?.[
                                            `${selectedAssetInfo.id}_${tracker.tracker}`
                                        ]}
                                    <div
                                        class="bg-white border border-slate-100 rounded-2xl p-5 shadow-sm space-y-4 hover:border-emerald-200/50 hover:shadow-md transition-all duration-300 relative overflow-hidden group"
                                    >
                                        <div
                                            class="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-emerald-400 to-teal-500 opacity-0 group-hover:opacity-100 transition-opacity"
                                        ></div>
                                        <div
                                            class="flex items-start justify-between gap-4"
                                        >
                                            <div class="min-w-0 flex-1">
                                                <h5
                                                    class="font-black text-slate-900 truncate text-sm"
                                                    title={tracker.tracker}
                                                >
                                                    {tracker.tracker ||
                                                        "Unnamed Tracker"}
                                                </h5>
                                                <p
                                                    class="text-[9px] text-slate-400 font-bold uppercase tracking-wider mt-0.5"
                                                >
                                                    TER: {tracker.ter || 0}%
                                                    {#if trackerYield !== undefined}
                                                        <span
                                                            class="mx-1 text-slate-300"
                                                            >•</span
                                                        >
                                                        <span
                                                            class="text-emerald-600 font-black"
                                                            >Yield: {trackerYield.toFixed(
                                                                2,
                                                            )}%</span
                                                        >
                                                    {/if}
                                                </p>
                                            </div>
                                            <span
                                                class="px-2 py-0.5 rounded-lg text-xs font-black bg-emerald-50 text-emerald-700 border border-emerald-100/50"
                                            >
                                                {(percentage * 100).toFixed(0)}%
                                            </span>
                                        </div>

                                        <!-- Progress Split Bar -->
                                        <div class="space-y-1.5">
                                            <div
                                                class="w-full h-1.5 bg-slate-100 rounded-full overflow-hidden"
                                            >
                                                <div
                                                    class="h-full bg-emerald-500 rounded-full transition-all duration-500"
                                                    style="width: {percentage *
                                                        100}%"
                                                ></div>
                                            </div>
                                        </div>

                                        <div
                                            class="grid grid-cols-3 gap-2 pt-3 border-t border-slate-50 text-left"
                                        >
                                            <div class="space-y-0.5">
                                                <span
                                                    class="text-slate-400 font-bold block text-[9px] uppercase tracking-wider"
                                                    >Ending Worth</span
                                                >
                                                <span
                                                    class="font-black text-slate-900 text-[10px] sm:text-xs truncate block"
                                                    title="€ {formatGermanAmount(
                                                        endingWorth,
                                                    )}"
                                                    >€ {formatGermanAmount(
                                                        endingWorth,
                                                    )}</span
                                                >
                                            </div>
                                            <div class="space-y-0.5">
                                                <span
                                                    class="text-slate-400 font-bold block text-[9px] uppercase tracking-wider"
                                                    >Total Inflow</span
                                                >
                                                <span
                                                    class="font-black text-indigo-600 text-[10px] sm:text-xs truncate block"
                                                    title="€ {formatGermanAmount(
                                                        totalCont,
                                                    )}"
                                                    >€ {formatGermanAmount(
                                                        totalCont,
                                                    )}</span
                                                >
                                            </div>
                                            <div class="space-y-0.5">
                                                <span
                                                    class="text-slate-400 font-bold block text-[9px] uppercase tracking-wider"
                                                    >Growth Share</span
                                                >
                                                <span
                                                    class="font-black text-emerald-600 text-[10px] sm:text-xs truncate block"
                                                    title="€ {formatGermanAmount(
                                                        totalGrowth,
                                                    )}"
                                                    >€ {formatGermanAmount(
                                                        totalGrowth,
                                                    )}</span
                                                >
                                            </div>
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        </div>
                    {/if}

                    <div class="flex-1 relative min-h-[350px]">
                        {#if assetChartData}
                            <Line
                                data={assetChartData}
                                options={assetChartOptions}
                            />
                        {:else}
                            <div
                                class="absolute inset-0 flex items-center justify-center text-slate-400 font-bold text-sm"
                            >
                                Preparing Asset Chart...
                            </div>
                        {/if}
                    </div>
                {:else}
                    <div
                        class="flex flex-col items-center justify-center p-20 border border-dashed border-slate-200 rounded-3xl space-y-4"
                    >
                        <PieChart class="w-12 h-12 text-slate-300" />
                        <div class="text-center space-y-1">
                            <h4
                                class="font-black text-slate-900 text-sm uppercase tracking-wider"
                            >
                                No Active Asset for Detailed Exploration
                            </h4>
                            <p
                                class="text-slate-400 font-medium text-xs max-w-md"
                            >
                                Please select a scenario that contains active
                                asset allocations to analyze their specific
                                growth, interest, and penalty behavior over
                                time.
                            </p>
                        </div>
                    </div>
                {/if}
            </div>

            <!-- Comparative Statistics Cards Grid -->
            <div
                class="md:col-span-12 space-y-6 pt-6 border-t border-slate-200"
            >
                <div class="flex items-center justify-between">
                    <div>
                        <h4
                            class="text-xl font-black text-slate-900 tracking-tight"
                        >
                            Key Performance Indicators
                        </h4>
                        <p class="text-slate-500 font-medium text-sm">
                            Comparative matrix of projected parameters at the {timeHorizonYears}-Year
                            horizon.
                        </p>
                    </div>
                </div>

                <div
                    class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6"
                >
                    {#each scenarioStatsList as stats}
                        <div
                            class="glass-card p-8 border relative overflow-hidden flex flex-col space-y-6"
                        >
                            <!-- Scenario Title header -->
                            <div
                                class="flex items-center justify-between pb-4 border-b border-slate-100"
                            >
                                <h4
                                    class="font-black text-slate-900 tracking-tight text-lg max-w-[70%] truncate"
                                >
                                    {stats.name}
                                </h4>
                                <span
                                    class="px-3 py-1 rounded-full text-[9px] font-black uppercase tracking-wider {stats
                                        .color.bgClass} border"
                                >
                                    Overlay Mode
                                </span>
                            </div>

                            <!-- Performance Stats -->
                            <div class="grid grid-cols-2 gap-6">
                                <div class="space-y-1">
                                    <span
                                        class="text-[9px] font-black text-slate-400 uppercase tracking-[0.2em] block"
                                        >Projected {activeMetric.replace(
                                            "_",
                                            " ",
                                        )}</span
                                    >
                                    <span
                                        class="font-black text-slate-900 text-lg"
                                    >
                                        € {formatGermanAmount(
                                            stats.endingMetric,
                                        )}
                                    </span>
                                </div>
                                <div class="space-y-1">
                                    <span
                                        class="text-[9px] font-black text-slate-400 uppercase tracking-[0.2em] block"
                                        >Ending Net Worth</span
                                    >
                                    <span
                                        class="font-black text-slate-900 text-lg"
                                    >
                                        € {formatGermanAmount(
                                            stats.endingNetWorth,
                                        )}
                                    </span>
                                </div>
                                <div class="space-y-1">
                                    <span
                                        class="text-[9px] font-black text-slate-400 uppercase tracking-[0.2em] block"
                                        >Peak Asset Worth</span
                                    >
                                    <span
                                        class="font-black text-emerald-600 text-lg"
                                    >
                                        € {formatGermanAmount(stats.peakAssets)}
                                    </span>
                                </div>
                                <div class="space-y-1">
                                    <span
                                        class="text-[9px] font-black text-slate-400 uppercase tracking-[0.2em] block"
                                        >Debt Freedom Date</span
                                    >
                                    <span
                                        class="font-black text-slate-900 text-lg flex items-center gap-1.5"
                                    >
                                        {#if stats.debtFreedomDate === "No Debt"}
                                            <span
                                                class="text-emerald-600 text-sm font-bold uppercase tracking-tight flex items-center gap-1"
                                            >
                                                <CheckCircle2
                                                    class="w-3.5 h-3.5"
                                                /> No Debt
                                            </span>
                                        {:else if stats.debtFreedomDate === "Never"}
                                            <span
                                                class="text-rose-500 text-sm font-bold uppercase tracking-tight"
                                                >Never</span
                                            >
                                        {:else}
                                            {stats.debtFreedomDate}
                                        {/if}
                                    </span>
                                </div>
                                <div class="space-y-1">
                                    <span
                                        class="text-[9px] font-black text-indigo-600 uppercase tracking-[0.2em] block"
                                        >FI Milestone ({scenarios.find(
                                            (s) => s.id === stats.id,
                                        )?.passiveIncomePercentage ||
                                            3.5}%)</span
                                    >
                                    <span
                                        class="font-black text-slate-900 text-lg flex items-center gap-1.5"
                                    >
                                        {#if stats.fiDate === "Never"}
                                            <span
                                                class="text-rose-500 text-sm font-bold uppercase tracking-tight"
                                                >Not Reached</span
                                            >
                                        {:else}
                                            <span
                                                class="text-indigo-600 flex items-center gap-1.5"
                                            >
                                                <Sparkles class="w-3.5 h-3.5" />
                                                {stats.fiDate}
                                            </span>
                                        {/if}
                                    </span>
                                </div>
                            </div>
                        </div>
                    {/each}
                </div>
            </div>

            <!-- Tax & Penalty Analysis -->
            <div
                class="md:col-span-12 space-y-6 pt-12 border-t border-slate-200"
            >
                <div class="flex items-center justify-between">
                    <div>
                        <h4
                            class="text-xl font-black text-slate-900 tracking-tight"
                        >
                            Tax & Penalty Analysis
                        </h4>
                        <p class="text-slate-500 font-medium text-sm">
                            Detailed log of individual asset lots being bought
                            or sold, including simulated tax/penalties.
                        </p>
                    </div>
                </div>

                <div class="grid grid-cols-1 gap-6">
                    {#each selectedScenarioIds as sid}
                        {#if projections[sid]?.penalty_events?.length > 0}
                            <div class="glass-card p-6 border overflow-hidden">
                                <div class="flex items-center justify-between mb-6">
                                    <div class="flex items-center gap-3">
                                        <div
                                            class="w-10 h-10 rounded-xl flex items-center justify-center {PALETTE[
                                                selectedScenarioIds.indexOf(sid) %
                                                    PALETTE.length
                                            ].bgClass} shadow-sm border"
                                        >
                                            <HandCoins class="w-5 h-5" />
                                        </div>
                                        <div>
                                            <h5 class="font-black text-slate-900">
                                                {projections[sid].scenario_name}
                                            </h5>
                                            <p
                                                class="text-[10px] text-slate-400 font-bold uppercase tracking-wider"
                                            >
                                                Lot Transaction History
                                            </p>
                                        </div>
                                    </div>

                                    <button
                                        onclick={() => exportTaxAnalysisCSV(sid)}
                                        class="flex items-center gap-2 px-4 py-2 bg-slate-900 text-white rounded-xl font-black text-[10px] uppercase tracking-wider hover:bg-indigo-600 transition-all active:scale-95 shadow-lg shadow-slate-200"
                                    >
                                        <Download class="w-3.5 h-3.5" />
                                        Export CSV
                                    </button>
                                </div>

                                <div class="overflow-x-auto">
                                    <table class="w-full text-left">
                                        <thead>
                                            <tr class="border-b border-slate-100">
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400"
                                                    >Date</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400"
                                                    >Asset</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400"
                                                    >Lot ID</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400"
                                                    >Type</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400 text-right"
                                                    >Amount</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400 text-right"
                                                    >Profit</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400 text-center"
                                                    >Hold</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400 text-right"
                                                    >Penalty/Tax</th
                                                >
                                                <th
                                                    class="pb-3 text-[9px] font-black uppercase tracking-widest text-slate-400 text-right"
                                                    >Net Impact</th
                                                >
                                            </tr>
                                        </thead>
                                        <tbody class="divide-y divide-slate-50">
                                            {#each projections[sid].penalty_events as event}
                                                <tr>
                                                    <td
                                                        class="py-4 text-xs font-bold text-slate-600"
                                                    >
                                                        {new Date(
                                                            event.date,
                                                        ).toLocaleDateString(
                                                            "de-DE",
                                                            {
                                                                month: "2-digit",
                                                                year: "numeric",
                                                            },
                                                        )}
                                                    </td>
                                                    <td
                                                        class="py-4 text-xs font-black text-slate-900"
                                                        >{event.assetName}</td
                                                    >
                                                    <td class="py-4">
                                                        <div
                                                            class="flex flex-col"
                                                        >
                                                            <span
                                                                class="text-[10px] font-mono font-bold text-slate-500"
                                                                >{event.lotId}</span
                                                            >
                                                            <span
                                                                class="text-[8px] text-slate-400"
                                                                >Created: {new Date(
                                                                    event.lotCreatedAt,
                                                                ).toLocaleDateString(
                                                                    "de-DE",
                                                                    {
                                                                        month: "2-digit",
                                                                        year: "numeric",
                                                                    },
                                                                )}</span
                                                            >
                                                        </div>
                                                    </td>
                                                    <td class="py-4">
                                                        <span
                                                            class="px-2 py-0.5 rounded text-[8px] font-black uppercase tracking-wider {event.type ===
                                                            'BUY'
                                                                ? 'bg-emerald-50 text-emerald-600 border border-emerald-100'
                                                                : 'bg-rose-50 text-rose-600 border border-rose-100'}"
                                                        >
                                                            {event.type}
                                                        </span>
                                                    </td>
                                                    <td
                                                        class="py-4 text-xs font-black text-right text-slate-900"
                                                    >
                                                        € {formatGermanAmount(
                                                            event.amount,
                                                        )}
                                                    </td>
                                                    <td
                                                        class="py-4 text-xs font-black text-right {event.interestGenerated >
                                                        0
                                                            ? 'text-emerald-600'
                                                            : 'text-slate-300'}"
                                                    >
                                                        {event.interestGenerated >
                                                        0
                                                            ? `+€ ${formatGermanAmount(
                                                                  event.interestGenerated,
                                                              )}`
                                                            : "€ 0,00"}
                                                    </td>
                                                    <td
                                                        class="py-4 text-xs font-bold text-center text-slate-500"
                                                    >
                                                        {event.type === "SELL"
                                                            ? `${event.monthsHeld}m`
                                                            : "-"}
                                                    </td>
                                                    <td
                                                        class="py-4 text-xs font-black text-right {event.penaltyPaid >
                                                        0
                                                            ? 'text-rose-500'
                                                            : 'text-slate-300'}"
                                                    >
                                                        {event.penaltyPaid > 0
                                                            ? `-€ ${formatGermanAmount(
                                                                  event.penaltyPaid,
                                                              )}`
                                                            : "€ 0,00"}
                                                    </td>
                                                    <td
                                                        class="py-4 text-xs font-black text-right {event.type ===
                                                        'BUY'
                                                            ? 'text-slate-900'
                                                            : 'text-emerald-600'}"
                                                    >
                                                        € {formatGermanAmount(
                                                            event.amount -
                                                                event.penaltyPaid,
                                                        )}
                                                    </td>
                                                </tr>
                                            {/each}
                                        </tbody>
                                    </table>
                                </div>
                            </div>
                        {:else if selectedScenarioIds.length > 0}
                             <div class="glass-card p-12 border flex flex-col items-center justify-center text-center space-y-4">
                                <div class="w-16 h-16 rounded-3xl bg-slate-50 flex items-center justify-center text-slate-300">
                                    <HandCoins class="w-8 h-8" />
                                </div>
                                <div class="max-w-xs">
                                    <h5 class="font-black text-slate-900 uppercase text-xs tracking-widest">No Lot Transactions</h5>
                                    <p class="text-slate-400 text-sm font-medium mt-1">This scenario doesn't have any asset withdrawals or lot creations yet.</p>
                                </div>
                            </div>
                        {/if}
                    {/each}
                </div>
            </div>
        </div>
    {/if}
</div>
